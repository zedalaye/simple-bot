package bot

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"bot/internal/core/database"
	"bot/internal/dashboard"
	"bot/internal/logger"
	"bot/internal/version"
)

// errorBannerWindow : au-delà de ce délai, on considère la dernière erreur comme
// périmée et on ne l'affiche plus dans /status (évite d'alarmer sur du résolu).
const errorBannerWindow = 15 * time.Minute

// DashboardSource adapte le Bot à l'interface dashboard.Source : il ne fait que
// lire l'état (prix, RSI, cycles, PnL) et exposer le contrôle pause/reprise.
// Aucune logique métier ici — uniquement de la collecte et de la mise en forme.
//
// Il implémente aussi telegram.Dashboard via BuyNow, réservé au canal Telegram.
type DashboardSource struct {
	bot *Bot
}

// NewDashboardSource construit la source de données des interfaces de supervision.
func NewDashboardSource(b *Bot) *DashboardSource {
	return &DashboardSource{bot: b}
}

func (d *DashboardSource) Status() (dashboard.StatusSnapshot, error) {
	b := d.bot

	snap := dashboard.StatusSnapshot{
		Version:   version.Version,
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

	// Ordres encore ouverts : seul le total nous intéresse, d'où LIMIT 0.
	if _, total, err := b.db.GetOrdersWithPagination(database.Pending, 0, 0); err == nil {
		snap.OpenOrders = total
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

func (d *DashboardSource) Cycles() ([]dashboard.CycleView, error) {
	b := d.bot

	cycles, err := b.db.GetCycles("active")
	if err != nil {
		return nil, err
	}

	views := make([]dashboard.CycleView, 0, len(cycles))
	for _, c := range cycles {
		views = append(views, dashboard.CycleView{
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

func (d *DashboardSource) PnL() (dashboard.PnLSnapshot, error) {
	b := d.bot

	snap := dashboard.PnLSnapshot{Quote: b.market.QuoteAsset}
	if stats, err := b.db.GetStats(); err == nil {
		snap.Completed, _ = stats["completed_cycles_count"].(int)
		snap.TotalProfit, _ = stats["total_profit"].(float64)
		snap.AvgProfit, _ = stats["average_profit"].(float64)
	}
	return snap, nil
}

func (d *DashboardSource) Balance() (dashboard.BalanceSnapshot, error) {
	b := d.bot

	balances, base, quote, price, err := b.FetchBalances()
	if err != nil {
		return dashboard.BalanceSnapshot{}, err
	}

	snap := dashboard.BalanceSnapshot{Exchange: b.Config.ExchangeName}

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
		amounts := balances[asset]
		line := dashboard.BalanceLine{Asset: asset}
		switch asset {
		case base:
			line.Amount = b.market.FormatAmount(amounts.Total)
			if amounts.Used > 0 {
				line.Locked = b.market.FormatAmount(amounts.Used)
			}
			if price > 0 {
				v := amounts.Total * price
				line.Value = fmt.Sprintf("≈ %.2f %s", v, quote)
				total += v
				hasTotal = true
			}
		case quote:
			line.Amount = fmt.Sprintf("%.2f", amounts.Total)
			if amounts.Used > 0 {
				line.Locked = fmt.Sprintf("%.2f", amounts.Used)
			}
			total += amounts.Total
			hasTotal = true
		default:
			// Autres actifs : pas de prix connu (le bot ne suit que la paire configurée).
			line.Amount = strconv.FormatFloat(amounts.Total, 'f', -1, 64)
			if amounts.Used > 0 {
				line.Locked = strconv.FormatFloat(amounts.Used, 'f', -1, 64)
			}
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

func (d *DashboardSource) Pause() error  { return d.bot.Pause() }
func (d *DashboardSource) Resume() error { return d.bot.Resume() }

// BuyNow déclenche un achat manuel immédiat et retourne un résumé lisible de l'ordre posé.
// Réservé au dashboard Telegram : il ne fait pas partie de dashboard.Source.
func (d *DashboardSource) BuyNow() (string, error) { return d.bot.ForceBuy() }
