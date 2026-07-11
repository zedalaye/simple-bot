// Commande backtest : rejoue une stratégie rsi_dca sur les bougies historiques
// stockées en base, en réutilisant le vrai code de décision (algorithms) et la
// vraie math d'indicateurs (market). Permet de balayer une grille de paramètres
// pour comparer leur fréquence de cycles, leur capital mobilisé et leur P&L.
// Package backtestcli implémente la sous-commande « backtest » (voir en-tête ci-dessous).
package backtestcli

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"bot/internal/algorithms"
	"bot/internal/backtest"
	"bot/internal/core/database"
	"bot/internal/loader"
)

// Main est le point d'entrée de la sous-commande « backtest ». La base analysée est
// celle de l'instance ciblée par le flag global --root (chdir géré en amont par le
// dispatcher), lue via loader.LoadOffline().
func Main(args []string) {
	log.SetOutput(os.Stderr)

	var (
		pair       = flag.String("pair", "", "Paire à backtester (défaut : TRADING_PAIR de l'instance)")
		priceTF    = flag.String("price-tf", "15m", "Timeframe du chemin de prix (granularité de simulation)")
		feePct     = flag.Float64("fee", 0.1, "Frais par côté en %% (0.1 = 0,1 %%)")
		strategyID = flag.Int("strategy-id", 0, "Partir d'une stratégie existante (0 = config par défaut)")
		fromStr    = flag.String("from", "", "Début (YYYY-MM-DD), optionnel")
		toStr      = flag.String("to", "", "Fin (YYYY-MM-DD), optionnel")
		buyTTLBars = flag.Int("buy-ttl-bars", 0, "Annule un achat non rempli après N bougies (0 = jamais)")

		// Axes de la grille (listes séparées par des virgules ; vide = valeur de base)
		gRSITF     = flag.String("rsi-tf", "", "RSI timeframe(s), ex: 15m,1h")
		gThreshold = flag.String("rsi-threshold", "", "Seuil(s) RSI, ex: 40,45,50")
		gProfit    = flag.String("profit", "", "Cible(s) de profit en %%, ex: 0.5,1,2")
		gInterval  = flag.String("interval", "", "Intervalle(s) d'achat en secondes, ex: 21600,43200,86400")

		// Surcharges simples (valeur unique)
		rsiPeriod  = flag.Int("rsi-period", 14, "Période RSI")
		volAdj     = flag.Float64("vol-adj", -1, "Ajustement volatilité en %% (-1 = garder la base)")
		volPeriod  = flag.Int("vol-period", 7, "Période volatilité")
		volTF      = flag.String("vol-tf", "4h", "Timeframe volatilité")
		trailing   = flag.Float64("trailing", 0.1, "Trailing stop delta en %%")
		sellOffset = flag.Float64("sell-offset", 0.1, "Offset de vente en %%")
		quote      = flag.Float64("quote", 20, "Montant par ordre (quote)")
		maxCycles  = flag.Int("max-cycles", 0, "Cycles concurrents max (0 = illimité)")
	)
	flag.CommandLine.Parse(args)

	cfg, db, err := loader.LoadOffline()
	if err != nil {
		log.Fatalf("Chargement de l'instance : %v", err)
	}
	defer db.Close()

	if *pair == "" {
		*pair = cfg.TradingPair
	}

	// Stratégie de base : existante ou défaut.
	base := defaultStrategy(*rsiPeriod, *volPeriod, *volTF)
	if *strategyID > 0 {
		s, err := db.GetStrategy(*strategyID)
		if err != nil || s == nil {
			log.Fatalf("Stratégie #%d introuvable : %v", *strategyID, err)
		}
		base = *s
	}
	// Surcharges simples
	base.AlgorithmName = "rsi_dca"
	base.RSIPeriod = iptr(*rsiPeriod)
	base.VolatilityPeriod = iptr(*volPeriod)
	base.VolatilityTimeframe = *volTF
	base.TrailingStopDelta = *trailing
	base.SellOffset = *sellOffset
	base.QuoteAmount = *quote
	base.MaxConcurrentCycles = *maxCycles
	base.CronExpression = "" // backtest piloté par intervalle (sauf override futur)
	if *volAdj >= 0 {
		base.VolatilityAdjustment = fptr(*volAdj)
	}

	// Charger toutes les bougies de la paire (toutes timeframes en base).
	tfs, err := db.GetCandleTimeframes(*pair)
	if err != nil {
		log.Fatalf("Lecture timeframes : %v", err)
	}
	candlesByTF := make(map[string][]database.Candle, len(tfs))
	for _, tf := range tfs {
		cs, err := db.GetAllCandles(*pair, tf)
		if err != nil {
			log.Fatalf("Lecture bougies %s/%s : %v", *pair, tf, err)
		}
		candlesByTF[tf] = cs
	}
	if len(candlesByTF[*priceTF]) == 0 {
		log.Fatalf("Aucune bougie %s/%s pour le chemin de prix", *pair, *priceTF)
	}

	startMs := parseDay(*fromStr, false)
	endMs := parseDay(*toStr, true)

	// Construire la grille.
	rsiTFs := listOrDefaultStr(*gRSITF, orStr(base.RSITimeframe, "15m"))
	thresholds := listOrDefaultFloat(*gThreshold, ptrOr(base.RSIThreshold, 45))
	profits := listOrDefaultFloat(*gProfit, orFloat(base.ProfitTarget, 1.0))
	intervals := listOrDefaultInt(*gInterval, orInt(base.BuyIntervalSeconds, 86400))

	precision := algorithms.MarketPrecision{Price: 0.01, Amount: 0.000001}

	fmt.Printf("Backtest %s — chemin de prix %s — frais %.2f%%/côté\n", *pair, *priceTF, *feePct)
	var results []backtest.Result
	var labels []string
	for _, tf := range rsiTFs {
		for _, th := range thresholds {
			for _, pf := range profits {
				for _, itv := range intervals {
					s := base
					s.RSITimeframe = tf
					s.RSIThreshold = fptr(th)
					s.ProfitTarget = pf
					s.BuyIntervalSeconds = itv
					cfg := backtest.Config{
						Pair:           *pair,
						Strategy:       s,
						PriceTimeframe: *priceTF,
						FeeRate:        *feePct / 100.0,
						Precision:      precision,
						StartMs:        startMs,
						EndMs:          endMs,
						BuyTTLBars:     *buyTTLBars,
					}
					r, err := backtest.Run(cfg, candlesByTF)
					if err != nil {
						log.Fatalf("Backtest (%s/%.0f/%.2f/%ds) : %v", tf, th, pf, itv, err)
					}
					results = append(results, r)
					adj := 0.0
					if s.VolatilityAdjustment != nil {
						adj = *s.VolatilityAdjustment
					}
					labels = append(labels, fmt.Sprintf("%s/%.0f/%.2g%%/adj%.0f/%dh", tf, th, pf, adj, itv/3600))
				}
			}
		}
	}

	// Tri par cycles/jour décroissant.
	idx := make([]int, len(results))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return results[idx[a]].CyclesPerDay > results[idx[b]].CyclesPerDay })

	if len(results) > 0 {
		fmt.Printf("Période : %.0f jours (%s → %s)\n\n",
			results[0].Days, msDay(results[0].StartMs), msDay(results[0].EndMs))
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "config\tachats/j\tcycles/j\tdur_med_j\tpic_cyc\tcapital_pic\tstock_fin\tgain_net\tlatent\ttotal\twin%\tnon_vendus")
	for _, i := range idx {
		r := results[i]
		// total = réalisé (cycles bouclés) + latent (stock invendu valorisé au dernier prix)
		total := r.RealizedPnL + r.UnrealizedPnL
		fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%.2f\t%d\t%.0f\t%.0f\t%.1f\t%+.0f\t%+.0f\t%.0f\t%d\n",
			labels[i], r.BuysPerDay, r.CyclesPerDay, r.MedianCycleDays, r.PeakOpenCycles,
			r.PeakCapital, r.OpenNotional, r.RealizedPnL, r.UnrealizedPnL, total,
			r.WinRate, r.CyclesOpenEnd)
	}
	w.Flush()
}

// ---- helpers ----

func defaultStrategy(rsiPeriod, volPeriod int, volTF string) database.Strategy {
	return database.Strategy{
		Name:                 "backtest",
		AlgorithmName:        "rsi_dca",
		Enabled:              true,
		RSIThreshold:         fptr(45),
		RSIPeriod:            iptr(rsiPeriod),
		RSITimeframe:         "15m",
		ProfitTarget:         1.0,
		TrailingStopDelta:    0.1,
		SellOffset:           0.1,
		VolatilityPeriod:     iptr(volPeriod),
		VolatilityAdjustment: fptr(0),
		VolatilityTimeframe:  volTF,
		QuoteAmount:          20,
		BuyIntervalSeconds:   86400,
	}
}

func iptr(v int) *int         { return &v }
func fptr(v float64) *float64 { return &v }

func ptrOr(p *float64, d float64) float64 {
	if p != nil {
		return *p
	}
	return d
}
func orStr(s, d string) string {
	if s != "" {
		return s
	}
	return d
}
func orFloat(v, d float64) float64 {
	if v != 0 {
		return v
	}
	return d
}
func orInt(v, d int) int {
	if v != 0 {
		return v
	}
	return d
}

func listOrDefaultStr(csv, def string) []string {
	if strings.TrimSpace(csv) == "" {
		return []string{def}
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func listOrDefaultFloat(csv string, def float64) []float64 {
	if strings.TrimSpace(csv) == "" {
		return []float64{def}
	}
	var out []float64
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				log.Fatalf("valeur numérique invalide %q", p)
			}
			out = append(out, v)
		}
	}
	return out
}
func listOrDefaultInt(csv string, def int) []int {
	if strings.TrimSpace(csv) == "" {
		return []int{def}
	}
	var out []int
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			v, err := strconv.Atoi(p)
			if err != nil {
				log.Fatalf("entier invalide %q", p)
			}
			out = append(out, v)
		}
	}
	return out
}

func parseDay(s string, endOfDay bool) int64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		log.Fatalf("date invalide %q (attendu YYYY-MM-DD)", s)
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Millisecond)
	}
	return t.UnixMilli()
}

func msDay(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}
