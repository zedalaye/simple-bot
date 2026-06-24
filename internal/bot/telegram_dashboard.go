package bot

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"bot/internal/logger"
	"bot/internal/telegram"
)

// errorBannerWindow : au-delà de ce délai, on considère la dernière erreur comme
// périmée et on ne l'affiche plus dans /status (évite d'alarmer sur du résolu).
const errorBannerWindow = 15 * time.Minute

// telegramDashboard adapte le Bot à l'interface telegram.Dashboard : il ne fait
// que lire l'état (prix, RSI, cycles, PnL) et exposer le contrôle pause/reprise.
// Aucune logique métier ici — uniquement de la collecte et de la mise en forme.
type telegramDashboard struct {
	bot *Bot
}

// NewTelegramDashboard construit la source de données du dashboard Telegram.
func NewTelegramDashboard(b *Bot) telegram.Dashboard {
	return &telegramDashboard{bot: b}
}

func (d *telegramDashboard) Status() (telegram.StatusSnapshot, error) {
	b := d.bot

	snap := telegram.StatusSnapshot{
		Exchange:  b.Config.ExchangeName,
		Pair:      b.Config.Pair,
		Quote:     b.market.QuoteAsset,
		Paused:    b.IsPaused(),
		UpdatedAt: time.Now(),
		RSI:       "n/a",
	}

	// Prix courant
	if price, err := b.exchange.GetPrice(b.Config.Pair); err != nil {
		snap.Price = "n/a"
	} else {
		snap.Price = b.market.FormatPrice(b.roundToPrecision(price, b.market.Precision.Price))
	}

	// RSI : première stratégie activée disposant d'une configuration RSI.
	if strategies, err := b.db.GetAllStrategies(); err == nil {
		for _, s := range strategies {
			if !s.Enabled || s.RSIPeriod == nil || *s.RSIPeriod <= 0 {
				continue
			}
			tf := s.RSITimeframe
			if tf == "" {
				tf = "4h"
			}
			if rsi, err := b.Calculator.CalculateRSI(b.Config.Pair, tf, *s.RSIPeriod); err == nil {
				snap.RSI = fmt.Sprintf("%.0f", rsi)
				snap.RSITimeframe = tf
			}
			break
		}
	}

	if stats, err := b.db.GetStats(); err == nil {
		snap.ActiveCycles, _ = stats["active_cycles_count"].(int)
		snap.TotalProfit, _ = stats["total_profit"].(float64)
		snap.AvgProfit, _ = stats["average_profit"].(float64)
	}

	// Cycles « open » : achat rempli, vente pas encore placée.
	if open, err := b.db.GetCycles("open"); err == nil {
		snap.OpenCycles = len(open)
	}

	// Heartbeat : uptime et fraîcheur du dernier price-check réussi.
	if !b.startedAt.IsZero() {
		snap.Uptime = time.Since(b.startedAt).Round(time.Second).String()
	}
	if last := b.lastCheck.Load(); last > 0 {
		snap.LastCheckAgo = time.Since(time.Unix(0, last)).Round(time.Second).String()
	}

	// Bannière d'erreur : uniquement si une erreur récente a été enregistrée.
	if msg, at, count := logger.LastError(); count > 0 && time.Since(at) < errorBannerWindow {
		snap.ErrorMsg = msg
		snap.ErrorAgo = time.Since(at).Round(time.Second).String()
	}

	return snap, nil
}

func (d *telegramDashboard) Cycles() ([]telegram.CycleView, error) {
	b := d.bot

	cycles, err := b.db.GetCycles("active")
	if err != nil {
		return nil, err
	}

	views := make([]telegram.CycleView, 0, len(cycles))
	for _, c := range cycles {
		views = append(views, telegram.CycleView{
			ID:       c.ID,
			Status:   string(c.Status),
			Amount:   b.market.FormatAmount(c.BuyOrder.Amount),
			BuyPrice: b.market.FormatPrice(c.BuyOrder.Price),
			Target:   b.market.FormatPrice(c.TargetPrice),
			Age:      time.Since(c.CreatedAt).Round(time.Minute).String(),
		})
	}
	return views, nil
}

func (d *telegramDashboard) PnL() (telegram.PnLSnapshot, error) {
	b := d.bot

	snap := telegram.PnLSnapshot{Quote: b.market.QuoteAsset}
	if stats, err := b.db.GetStats(); err == nil {
		snap.Completed, _ = stats["completed_cycles_count"].(int)
		snap.TotalProfit, _ = stats["total_profit"].(float64)
		snap.AvgProfit, _ = stats["average_profit"].(float64)
	}
	return snap, nil
}

func (d *telegramDashboard) Balance() (telegram.BalanceSnapshot, error) {
	b := d.bot

	balances, base, quote, price, err := b.FetchBalances()
	if err != nil {
		return telegram.BalanceSnapshot{}, err
	}

	snap := telegram.BalanceSnapshot{Exchange: b.Config.ExchangeName}

	// Ordre stable : base, puis quote, puis le reste trié alphabétiquement.
	assets := make([]string, 0, len(balances))
	for a := range balances {
		assets = append(assets, a)
	}
	sort.SliceStable(assets, func(i, j int) bool {
		ri, rj := assetRank(assets[i], base, quote), assetRank(assets[j], base, quote)
		if ri != rj {
			return ri < rj
		}
		return assets[i] < assets[j]
	})

	var total float64
	var hasTotal bool
	for _, asset := range assets {
		amount := balances[asset]
		line := telegram.BalanceLine{Asset: asset}
		switch asset {
		case base:
			line.Amount = b.market.FormatAmount(amount)
			if price > 0 {
				v := amount * price
				line.Value = fmt.Sprintf("≈ %.2f %s", v, quote)
				total += v
				hasTotal = true
			}
		case quote:
			line.Amount = fmt.Sprintf("%.2f", amount)
			total += amount
			hasTotal = true
		default:
			// Autres actifs : pas de prix connu (le bot ne suit que la paire configurée).
			line.Amount = strconv.FormatFloat(amount, 'f', -1, 64)
		}
		snap.Lines = append(snap.Lines, line)
	}

	if hasTotal {
		snap.Total = fmt.Sprintf("%.2f %s", total, quote)
	}
	return snap, nil
}

// assetRank ordonne les actifs : base (0), quote (1), reste (2).
func assetRank(asset, base, quote string) int {
	switch asset {
	case base:
		return 0
	case quote:
		return 1
	default:
		return 2
	}
}

func (d *telegramDashboard) Pause() error  { return d.bot.Pause() }
func (d *telegramDashboard) Resume() error { return d.bot.Resume() }

// BuyNow déclenche un achat manuel immédiat et retourne un résumé lisible de l'ordre posé.
func (d *telegramDashboard) BuyNow() (string, error) { return d.bot.ForceBuy() }
