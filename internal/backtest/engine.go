package backtest

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"bot/internal/algorithms"
	"bot/internal/core/database"
	"bot/internal/logger"

	"github.com/robfig/cron/v3"
)

// Config paramètre une exécution de backtest.
type Config struct {
	Pair           string
	Strategy       database.Strategy
	PriceTimeframe string                     // chemin de prix simulé (ex. "15m")
	FeeRate        float64                    // frais par côté en fraction (0.001 = 0,1 %)
	Precision      algorithms.MarketPrecision // précision marché pour l'arrondi des ordres
	StartMs, EndMs int64                      // bornes temporelles (0 = toute la plage dispo)
	BuyTTLBars     int                        // annule un achat non rempli après N bougies (0 = jamais)
}

// Result agrège les métriques d'un backtest.
type Result struct {
	Pair, PriceTimeframe string
	StartMs, EndMs       int64
	Days                 float64
	BuysPlaced           int
	BuysFilled           int
	BuysCancelled        int
	CyclesClosed         int
	CyclesOpenEnd        int
	BuysPerDay           float64
	CyclesPerDay         float64
	MedianCycleDays      float64
	MeanCycleDays        float64
	PeakOpenCycles       int
	PeakCapital          float64 // notional max simultanément investi (quote)
	InvestedClosed       float64 // notional total des cycles bouclés (quote)
	RealizedPnL          float64 // quote, net de frais
	RealizedReturnPct    float64 // RealizedPnL / InvestedClosed * 100
	Fees                 float64
	WinRate              float64 // % de cycles bouclés à PnL > 0
	UnrealizedPnL        float64 // P&L latent des cycles ouverts au dernier prix
	OpenNotional         float64 // notional investi encore détenu (cycles non vendus)
	FinalPrice           float64
}

// simCycle est l'état interne d'un cycle pendant la simulation.
type simCycle struct {
	id         int
	buyLimit   float64
	amount     float64
	target     float64
	placedBar  int
	buyFilled  bool
	buyPrice   float64
	buyFillMs  int64
	maxPrice   float64
	sellPlaced bool
	sellLimit  float64
	closed     bool
}

// Run exécute le backtest et renvoie le résultat agrégé.
//
// Modèle (fidèle à la boucle de tick du bot) : chaque bougie du chemin de prix
// est un « tick ». Les décisions sont prises à la CLÔTURE de la bougie (pas de
// look-ahead : indicateurs sur bougies déjà clôturées). Un ordre posé au tick i
// ne peut se remplir qu'à partir du tick i+1, via le bas/haut des bougies
// suivantes — comme un ordre limite qui attend que le marché vienne le toucher.
func Run(cfg Config, candlesByTF map[string][]database.Candle) (Result, error) {
	// Les algorithmes journalisent via le logger global ; en contexte hors-ligne
	// (tests, embarqué) il peut ne pas être initialisé -> on le coupe au niveau
	// error pour éviter tout nil deref et tout bruit.
	if !logger.IsInitialized() {
		_ = logger.InitLogger("error", "")
	}

	algo, ok := algorithms.NewAlgorithmRegistry().Get(cfg.Strategy.AlgorithmName)
	if !ok {
		return Result{}, fmt.Errorf("algorithme %q introuvable", cfg.Strategy.AlgorithmName)
	}
	if err := algo.ValidateConfig(cfg.Strategy); err != nil {
		return Result{}, fmt.Errorf("config stratégie invalide : %w", err)
	}

	price := candlesByTF[cfg.PriceTimeframe]
	if len(price) == 0 {
		return Result{}, fmt.Errorf("aucune bougie pour le chemin de prix %s/%s", cfg.Pair, cfg.PriceTimeframe)
	}
	price = append([]database.Candle(nil), price...)
	sort.Slice(price, func(i, j int) bool { return price[i].Timestamp < price[j].Timestamp })
	dur := TimeframeMillis(cfg.PriceTimeframe)

	calc := NewCalculator(cfg.Pair, candlesByTF)

	// Déclencheur d'achat : intervalle (mode périodique) ou cron.
	trigger, err := newBuyTrigger(cfg.Strategy, price[0].Timestamp, price[len(price)-1].Timestamp+dur)
	if err != nil {
		return Result{}, err
	}

	ctx := algorithms.TradingContext{
		ExchangeName: "backtest",
		Pair:         cfg.Pair,
		Calculator:   calc,
		Precision:    cfg.Precision,
	}

	var (
		open      []*simCycle
		nextID    int
		res       = Result{Pair: cfg.Pair, PriceTimeframe: cfg.PriceTimeframe}
		durations []float64
		wins      int
		firstMs   int64 = -1
		lastClose float64
		lastMs    int64
	)

	for i := range price {
		c := price[i]
		closeMs := c.Timestamp + dur
		if cfg.StartMs > 0 && closeMs < cfg.StartMs {
			continue
		}
		if cfg.EndMs > 0 && closeMs > cfg.EndMs {
			break
		}
		if firstMs < 0 {
			firstMs = closeMs
		}
		lastMs = closeMs
		lastClose = c.ClosePrice
		calc.SetNow(closeMs)
		ctx.CurrentPrice = c.ClosePrice

		// 1) Remplissage des ordres posés AUX TICKS PRÉCÉDENTS.
		kept := open[:0]
		for _, cy := range open {
			if !cy.buyFilled {
				if c.LowPrice <= cy.buyLimit {
					cy.buyFilled = true
					cy.buyPrice = cy.buyLimit
					cy.buyFillMs = closeMs
					cy.maxPrice = c.ClosePrice
					res.BuysFilled++
					res.Fees += cy.buyPrice * cy.amount * cfg.FeeRate
				} else if cfg.BuyTTLBars > 0 && i-cy.placedBar >= cfg.BuyTTLBars {
					res.BuysCancelled++
					continue // ordre annulé, cycle abandonné
				} else {
					kept = append(kept, cy)
					continue
				}
			} else if cy.sellPlaced {
				if c.HighPrice >= cy.sellLimit {
					cy.closed = true
					gross := (cy.sellLimit - cy.buyPrice) * cy.amount
					res.Fees += cy.sellLimit * cy.amount * cfg.FeeRate
					pnl := gross - cy.buyPrice*cy.amount*cfg.FeeRate - cy.sellLimit*cy.amount*cfg.FeeRate
					res.RealizedPnL += pnl
					res.InvestedClosed += cy.buyPrice * cy.amount
					if pnl > 0 {
						wins++
					}
					durations = append(durations, float64(closeMs-cy.buyFillMs)/86400000.0)
					res.CyclesClosed++
					continue // cycle bouclé
				}
			}
			kept = append(kept, cy)
		}
		open = kept

		// 2) Mise à jour du plus-haut + 3) évaluation des ventes (cycles à achat rempli).
		for _, cy := range open {
			if !cy.buyFilled || cy.sellPlaced {
				continue
			}
			if c.HighPrice > cy.maxPrice {
				cy.maxPrice = c.HighPrice
			}
			dbCycle := database.Cycle{ID: cy.id, TargetPrice: cy.target, MaxPrice: cy.maxPrice}
			sig, err := algo.ShouldSell(ctx, dbCycle, cfg.Strategy)
			if err == nil && sig.ShouldSell {
				cy.sellPlaced = true
				cy.sellLimit = sig.LimitPrice
			}
		}

		// 4) Déclencheur d'achat.
		if trigger.fires(closeMs) {
			active := 0
			for _, cy := range open {
				if !cy.closed {
					active++
				}
			}
			withinCap := cfg.Strategy.MaxConcurrentCycles <= 0 || active < cfg.Strategy.MaxConcurrentCycles
			if withinCap {
				sig, err := algo.ShouldBuy(ctx, cfg.Strategy)
				if err == nil && sig.ShouldBuy {
					nextID++
					open = append(open, &simCycle{
						id:        nextID,
						buyLimit:  sig.LimitPrice,
						amount:    sig.Amount,
						target:    sig.TargetPrice,
						placedBar: i,
					})
					res.BuysPlaced++
					trigger.consume(closeMs)
				}
			}
		}

		// Suivi du capital (notional des cycles à achat rempli, non bouclés).
		capital := 0.0
		liveOpen := 0
		for _, cy := range open {
			if cy.buyFilled && !cy.closed {
				capital += cy.buyPrice * cy.amount
				liveOpen++
			}
		}
		if capital > res.PeakCapital {
			res.PeakCapital = capital
		}
		if liveOpen > res.PeakOpenCycles {
			res.PeakOpenCycles = liveOpen
		}
	}

	// Agrégats finaux.
	res.StartMs, res.EndMs = firstMs, lastMs
	res.FinalPrice = lastClose
	if firstMs > 0 && lastMs > firstMs {
		res.Days = float64(lastMs-firstMs) / 86400000.0
	}
	if res.Days > 0 {
		res.BuysPerDay = float64(res.BuysFilled) / res.Days
		res.CyclesPerDay = float64(res.CyclesClosed) / res.Days
	}
	if len(durations) > 0 {
		sort.Float64s(durations)
		res.MedianCycleDays = durations[len(durations)/2]
		sum := 0.0
		for _, d := range durations {
			sum += d
		}
		res.MeanCycleDays = sum / float64(len(durations))
	}
	if res.InvestedClosed > 0 {
		res.RealizedReturnPct = res.RealizedPnL / res.InvestedClosed * 100
	}
	if res.CyclesClosed > 0 {
		res.WinRate = float64(wins) / float64(res.CyclesClosed) * 100
	}
	for _, cy := range open {
		if cy.buyFilled && !cy.closed {
			res.CyclesOpenEnd++
			res.UnrealizedPnL += (lastClose - cy.buyPrice) * cy.amount
			res.OpenNotional += cy.buyPrice * cy.amount
		}
	}
	if math.IsNaN(res.RealizedReturnPct) {
		res.RealizedReturnPct = 0
	}
	return res, nil
}

// buyTrigger décide à quels instants une tentative d'achat est autorisée.
type buyTrigger struct {
	intervalMs int64
	lastBuyMs  int64
	cronTimes  []int64 // mode cron : instants de déclenchement triés
	cronIdx    int
}

func newBuyTrigger(s database.Strategy, startMs, endMs int64) (*buyTrigger, error) {
	if strings.TrimSpace(s.CronExpression) != "" {
		sched, err := cron.ParseStandard(s.CronExpression)
		if err != nil {
			return nil, fmt.Errorf("expression cron invalide %q : %w", s.CronExpression, err)
		}
		var times []int64
		t := time.UnixMilli(startMs).Local()
		for {
			t = sched.Next(t)
			ms := t.UnixMilli()
			if ms > endMs {
				break
			}
			times = append(times, ms)
		}
		return &buyTrigger{cronTimes: times}, nil
	}
	if s.BuyIntervalSeconds <= 0 {
		return nil, fmt.Errorf("la stratégie n'a ni cron ni buy_interval_seconds > 0")
	}
	return &buyTrigger{intervalMs: int64(s.BuyIntervalSeconds) * 1000, lastBuyMs: math.MinInt64 / 2}, nil
}

// fires indique si une tentative d'achat est autorisée à l'instant nowMs.
func (t *buyTrigger) fires(nowMs int64) bool {
	if t.cronTimes != nil {
		return t.cronIdx < len(t.cronTimes) && nowMs >= t.cronTimes[t.cronIdx]
	}
	return nowMs-t.lastBuyMs >= t.intervalMs
}

// consume enregistre qu'un achat a été posé à nowMs (réarme le cooldown / avance
// le curseur cron jusqu'au prochain créneau futur).
func (t *buyTrigger) consume(nowMs int64) {
	if t.cronTimes != nil {
		for t.cronIdx < len(t.cronTimes) && t.cronTimes[t.cronIdx] <= nowMs {
			t.cronIdx++
		}
		return
	}
	t.lastBuyMs = nowMs
}
