// Package chat implémente l'agent conseiller : un client Claude qui, via des
// « outils » (tool use), peut interroger la base de l'instance et lancer des
// backtests sur le vrai moteur pour étayer ses suggestions d'amélioration de
// stratégie. Les outils sont STRICTEMENT en lecture / simulation : aucun ne
// passe d'ordre réel ni ne modifie la base.
package chat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"bot/internal/algorithms"
	"bot/internal/backtest"
	"bot/internal/core/database"

	"github.com/anthropics/anthropic-sdk-go"
)

// precision utilisée pour l'arrondi des ordres simulés (identique à la commande
// backtest). MEXC/USDT : pas de prix 0,01, pas de quantité 1e-6.
var precision = algorithms.MarketPrecision{Price: 0.01, Amount: 0.000001}

// toolDefs déclare le catalogue d'outils exposés à Claude. Chaque entrée est un
// schéma JSON : Claude lit le nom + la description pour décider quand l'appeler,
// et le schéma d'entrée pour formater ses arguments.
func toolDefs() []anthropic.ToolUnionParam {
	tool := func(name, desc string, props map[string]any, required []string) anthropic.ToolUnionParam {
		return anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        name,
			Description: anthropic.String(desc),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: props, Required: required},
		}}
	}

	return []anthropic.ToolUnionParam{
		tool("list_strategies",
			"Liste toutes les stratégies de l'instance (id, nom, algorithme, activée ou non, quelques paramètres clés). À appeler en premier pour découvrir les stratégies disponibles.",
			map[string]any{}, nil),

		tool("get_strategy_config",
			"Renvoie la configuration complète d'une stratégie (tous les paramètres RSI, cibles, volatilité, filtres de tendance, sizing, intervalle d'achat...).",
			map[string]any{
				"strategy_id": map[string]any{"type": "integer", "description": "Identifiant de la stratégie"},
			},
			[]string{"strategy_id"}),

		tool("get_performance_stats",
			"Statistiques de performance réelles observées en base. Sans strategy_id : agrégats globaux (profit total/moyen, nombre de cycles/ordres). Avec strategy_id : stats de cette stratégie.",
			map[string]any{
				"strategy_id": map[string]any{"type": "integer", "description": "Optionnel : restreindre à une stratégie"},
			},
			nil),

		tool("sync_candles",
			"Récupère les dernières bougies OHLCV d'une timeframe DEPUIS L'EXCHANGE (lecture seule) et les enregistre en base, pour rafraîchir les données avant une analyse ou un backtest. À utiliser quand la fraîcheur compte (conditions récentes, prix actuel) car les données en base peuvent dater si le bot est éteint. Renvoie le nombre de bougies récupérées et le dernier prix de clôture.",
			map[string]any{
				"timeframe": map[string]any{"type": "string", "description": "Timeframe à rafraîchir (ex: 15m, 1h, 4h)"},
				"limit":     map[string]any{"type": "integer", "description": "Nombre de bougies à récupérer (défaut 200, max 1000)"},
			},
			[]string{"timeframe"}),

		tool("get_market_snapshot",
			"Calcule les indicateurs techniques COURANTS (RSI, volatilité, tendance via EMA rapide/lente) sur les bougies en base, avec exactement la même math que les stratégies. Avec strategy_id : utilise les timeframes/périodes configurés de cette stratégie (ce qu'elle « voit » maintenant). Signale si les données datent — pense à sync_candles d'abord si la fraîcheur compte.",
			map[string]any{
				"strategy_id": map[string]any{"type": "integer", "description": "Optionnel : utiliser les timeframes/périodes de cette stratégie"},
				"timeframe":   map[string]any{"type": "string", "description": "Optionnel : forcer le timeframe du RSI et de la tendance"},
				"rsi_period":  map[string]any{"type": "integer", "description": "Optionnel : forcer la période RSI"},
			},
			nil),

		tool("sweep_backtest",
			"Backteste une GRILLE de variantes d'une stratégie en un seul appel et renvoie une table markdown triée par PnL net. Chaque axe est une liste ; un axe omis garde la valeur de base. Max 24 combinaisons. À préférer à des appels run_backtest répétés quand on compare plusieurs réglages.",
			map[string]any{
				"strategy_id":           map[string]any{"type": "integer", "description": "Stratégie de base"},
				"rsi_timeframes":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Timeframes RSI à tester, ex: [\"15m\",\"1h\"]"},
				"rsi_thresholds":        map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "Seuils RSI à tester, ex: [40,45,50]"},
				"profit_targets":        map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "Cibles de profit en %% à tester"},
				"buy_intervals_seconds": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Intervalles d'achat en secondes à tester"},
				"price_timeframe":       map[string]any{"type": "string", "description": "Granularité du chemin de prix (défaut 15m)"},
				"fee_pct":               map[string]any{"type": "number", "description": "Frais par côté en %% (défaut 0.1)"},
				"from":                  map[string]any{"type": "string", "description": "Début YYYY-MM-DD (optionnel)"},
				"to":                    map[string]any{"type": "string", "description": "Fin YYYY-MM-DD (optionnel)"},
			},
			[]string{"strategy_id"}),

		tool("run_backtest",
			"Rejoue une stratégie sur les bougies historiques en base et renvoie les métriques (cycles/jour, capital de pic, PnL réalisé/latent, win rate...). Part d'une stratégie existante (strategy_id) puis applique les surcharges fournies. C'est l'outil clé pour comparer des variantes de paramètres AVANT de recommander un changement.",
			map[string]any{
				"strategy_id":           map[string]any{"type": "integer", "description": "Stratégie de base à backtester"},
				"rsi_timeframe":         map[string]any{"type": "string", "description": "Surcharge du timeframe RSI (ex: 15m, 1h)"},
				"rsi_threshold":         map[string]any{"type": "number", "description": "Surcharge du seuil RSI (ex: 45)"},
				"profit_target":         map[string]any{"type": "number", "description": "Surcharge de la cible de profit en %% (ex: 1.0)"},
				"buy_interval_seconds":  map[string]any{"type": "integer", "description": "Surcharge de l'intervalle d'achat en secondes (ex: 43200 pour 12h)"},
				"quote_amount":          map[string]any{"type": "number", "description": "Surcharge du montant par ordre (quote)"},
				"max_concurrent_cycles": map[string]any{"type": "integer", "description": "Surcharge du plafond de cycles simultanés (0 = illimité)"},
				"price_timeframe":       map[string]any{"type": "string", "description": "Granularité de simulation du chemin de prix (défaut 15m)"},
				"fee_pct":               map[string]any{"type": "number", "description": "Frais par côté en %% (défaut 0.1)"},
				"from":                  map[string]any{"type": "string", "description": "Date de début YYYY-MM-DD (optionnel)"},
				"to":                    map[string]any{"type": "string", "description": "Date de fin YYYY-MM-DD (optionnel)"},
			},
			[]string{"strategy_id"}),
	}
}

// dispatch exécute l'outil demandé par Claude et renvoie le résultat (texte
// JSON) + un flag d'erreur. rawInput est le JSON brut des arguments produits par
// le modèle.
func (a *Agent) dispatch(name, rawInput string) (string, bool) {
	switch name {
	case "list_strategies":
		return a.toolListStrategies()
	case "get_strategy_config":
		return a.toolGetStrategyConfig(rawInput)
	case "get_performance_stats":
		return a.toolGetPerformanceStats(rawInput)
	case "sync_candles":
		return a.toolSyncCandles(rawInput)
	case "get_market_snapshot":
		return a.toolMarketSnapshot(rawInput)
	case "sweep_backtest":
		return a.toolSweepBacktest(rawInput)
	case "run_backtest":
		return a.toolRunBacktest(rawInput)
	default:
		return fmt.Sprintf("outil inconnu : %q", name), true
	}
}

// jsonResult sérialise n'importe quelle valeur en JSON indenté pour la renvoyer
// à Claude ; en cas d'échec de sérialisation, renvoie l'erreur en tant que
// résultat d'outil (is_error).
func jsonResult(v any) (string, bool) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("erreur de sérialisation : %v", err), true
	}
	return string(b), false
}

// loadCandles charge toutes les bougies de la paire (toutes timeframes en base),
// prêtes pour le moteur de backtest ou le calculateur d'indicateurs.
func (a *Agent) loadCandles() (map[string][]database.Candle, error) {
	tfs, err := a.db.GetCandleTimeframes(a.pair)
	if err != nil {
		return nil, fmt.Errorf("lecture des timeframes : %w", err)
	}
	m := make(map[string][]database.Candle, len(tfs))
	for _, tf := range tfs {
		cs, err := a.db.GetAllCandles(a.pair, tf)
		if err != nil {
			return nil, fmt.Errorf("lecture bougies %s/%s : %w", a.pair, tf, err)
		}
		m[tf] = cs
	}
	return m, nil
}

func (a *Agent) toolListStrategies() (string, bool) {
	strategies, err := a.db.GetAllStrategies()
	if err != nil {
		return fmt.Sprintf("erreur DB : %v", err), true
	}
	type row struct {
		ID                 int     `json:"id"`
		Name               string  `json:"name"`
		Algorithm          string  `json:"algorithm"`
		Enabled            bool    `json:"enabled"`
		RSITimeframe       string  `json:"rsi_timeframe,omitempty"`
		RSIThreshold       *float64 `json:"rsi_threshold,omitempty"`
		ProfitTarget       float64 `json:"profit_target"`
		BuyIntervalSeconds int     `json:"buy_interval_seconds"`
		MaxConcurrent      int     `json:"max_concurrent_cycles"`
	}
	out := make([]row, 0, len(strategies))
	for _, s := range strategies {
		out = append(out, row{
			ID: s.ID, Name: s.Name, Algorithm: s.AlgorithmName, Enabled: s.Enabled,
			RSITimeframe: s.RSITimeframe, RSIThreshold: s.RSIThreshold,
			ProfitTarget: s.ProfitTarget, BuyIntervalSeconds: s.BuyIntervalSeconds,
			MaxConcurrent: s.MaxConcurrentCycles,
		})
	}
	return jsonResult(map[string]any{"pair": a.pair, "strategies": out})
}

func (a *Agent) toolGetStrategyConfig(rawInput string) (string, bool) {
	var in struct {
		StrategyID int `json:"strategy_id"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("arguments invalides : %v", err), true
	}
	s, err := a.db.GetStrategy(in.StrategyID)
	if err != nil || s == nil {
		return fmt.Sprintf("stratégie #%d introuvable : %v", in.StrategyID, err), true
	}
	return jsonResult(s)
}

func (a *Agent) toolGetPerformanceStats(rawInput string) (string, bool) {
	var in struct {
		StrategyID *int `json:"strategy_id"`
	}
	_ = json.Unmarshal([]byte(rawInput), &in) // arguments optionnels

	if in.StrategyID != nil {
		stats, err := a.db.GetStrategyStats(*in.StrategyID)
		if err != nil {
			return fmt.Sprintf("erreur DB : %v", err), true
		}
		return jsonResult(stats)
	}

	stats, err := a.db.GetStats()
	if err != nil {
		return fmt.Sprintf("erreur DB : %v", err), true
	}
	avg, total, err := a.db.GetProfitStats()
	if err == nil {
		stats["avg_profit"] = avg
		stats["total_profit"] = total
	}
	return jsonResult(stats)
}

func (a *Agent) toolSyncCandles(rawInput string) (string, bool) {
	var in struct {
		Timeframe string `json:"timeframe"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("arguments invalides : %v", err), true
	}
	if in.Timeframe == "" {
		return "le paramètre timeframe est requis (ex: 15m, 1h)", true
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	ex, err := a.getExchange()
	if err != nil {
		return err.Error(), true
	}
	// Lecture seule : on récupère les bougies publiques puis on les persiste.
	// SaveCandle est idempotent (INSERT OR IGNORE) : aucun doublon.
	candles, err := ex.FetchCandles(a.pair, in.Timeframe, nil, int64(limit))
	if err != nil {
		return fmt.Sprintf("échec du fetch OHLCV %s/%s : %v", a.pair, in.Timeframe, err), true
	}
	if len(candles) == 0 {
		return fmt.Sprintf("aucune bougie renvoyée par l'exchange pour %s/%s", a.pair, in.Timeframe), true
	}
	saved := 0
	for _, c := range candles {
		if err := a.db.SaveCandle(a.pair, in.Timeframe, c.Timestamp, c.Open, c.High, c.Low, c.Close, c.Volume); err != nil {
			continue
		}
		saved++
	}
	last := candles[len(candles)-1]
	return jsonResult(map[string]any{
		"pair":            a.pair,
		"timeframe":       in.Timeframe,
		"fetched":         len(candles),
		"saved":           saved,
		"latest_close":    last.Close,
		"latest_bar_time": time.UnixMilli(last.Timestamp).UTC().Format("2006-01-02 15:04 UTC"),
		"latest_bar_ms":   last.Timestamp,
	})
}

func (a *Agent) toolMarketSnapshot(rawInput string) (string, bool) {
	var in struct {
		StrategyID *int   `json:"strategy_id"`
		Timeframe  string `json:"timeframe"`
		RSIPeriod  int    `json:"rsi_period"`
	}
	_ = json.Unmarshal([]byte(rawInput), &in) // tous les champs sont optionnels

	// Paramètres par défaut (snapshot générique).
	rsiTF, rsiPeriod := "15m", 14
	volTF, volPeriod := "4h", 7
	emaTF, emaFast, emaSlow := "15m", 9, 21

	// Avec une stratégie : on calque ses timeframes/périodes -> « ce qu'elle voit ».
	if in.StrategyID != nil {
		s, err := a.db.GetStrategy(*in.StrategyID)
		if err != nil || s == nil {
			return fmt.Sprintf("stratégie #%d introuvable : %v", *in.StrategyID, err), true
		}
		if s.RSITimeframe != "" {
			rsiTF, emaTF = s.RSITimeframe, s.RSITimeframe
		}
		if s.RSIPeriod != nil {
			rsiPeriod = *s.RSIPeriod
		}
		if s.VolatilityTimeframe != "" {
			volTF = s.VolatilityTimeframe
		}
		if s.VolatilityPeriod != nil {
			volPeriod = *s.VolatilityPeriod
		}
		if s.TrendFilterTimeframe != "" {
			emaTF = s.TrendFilterTimeframe
		}
		if s.TrendFilterFastPeriod != nil {
			emaFast = *s.TrendFilterFastPeriod
		}
		if s.TrendFilterSlowPeriod != nil {
			emaSlow = *s.TrendFilterSlowPeriod
		}
	}
	if in.Timeframe != "" {
		rsiTF = in.Timeframe
	}
	if in.RSIPeriod > 0 {
		rsiPeriod = in.RSIPeriod
	}

	candlesByTF, err := a.loadCandles()
	if err != nil {
		return err.Error(), true
	}
	// Même calculateur que le backtest / le bot en live : indicateurs « as-of » now.
	calc := backtest.NewCalculator(a.pair, candlesByTF)
	calc.SetNow(time.Now().UnixMilli())

	// num renvoie la valeur, ou "n/a" si l'indicateur manque de données.
	num := func(v float64, err error) any {
		if err != nil {
			return "n/a"
		}
		return v
	}

	snapshot := map[string]any{
		"pair":       a.pair,
		"as_of":      time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		"rsi":        map[string]any{"timeframe": rsiTF, "period": rsiPeriod, "value": num(calc.CalculateRSI(a.pair, rsiTF, rsiPeriod))},
		"volatility": map[string]any{"timeframe": volTF, "period": volPeriod, "value": num(calc.CalculateVolatility(a.pair, volTF, volPeriod))},
	}

	ef, efErr := calc.CalculateEMA(a.pair, emaTF, emaFast)
	es, esErr := calc.CalculateEMA(a.pair, emaTF, emaSlow)
	trend := map[string]any{
		"timeframe": emaTF,
		"ema_fast":  map[string]any{"period": emaFast, "value": num(ef, efErr)},
		"ema_slow":  map[string]any{"period": emaSlow, "value": num(es, esErr)},
	}
	if efErr == nil && esErr == nil {
		if ef > es {
			trend["direction"] = "haussière (EMA rapide > EMA lente)"
		} else {
			trend["direction"] = "baissière (EMA rapide <= EMA lente)"
		}
	}
	snapshot["trend"] = trend

	// Dernier prix + avertissement de fraîcheur.
	if last, err := a.db.GetLastCandle(a.pair, rsiTF); err == nil && last != nil {
		snapshot["last_close"] = last.ClosePrice
		snapshot["last_bar_time"] = time.UnixMilli(last.Timestamp).UTC().Format("2006-01-02 15:04 UTC")
		if time.Since(time.UnixMilli(last.Timestamp)) > 6*time.Hour {
			snapshot["freshness_warning"] = "les bougies en base datent de plus de 6h ; utilise sync_candles avant de conclure"
		}
	}
	return jsonResult(snapshot)
}

func (a *Agent) toolSweepBacktest(rawInput string) (string, bool) {
	var in struct {
		StrategyID     int       `json:"strategy_id"`
		RSITimeframes  []string  `json:"rsi_timeframes"`
		RSIThresholds  []float64 `json:"rsi_thresholds"`
		ProfitTargets  []float64 `json:"profit_targets"`
		BuyIntervals   []int     `json:"buy_intervals_seconds"`
		PriceTimeframe string    `json:"price_timeframe"`
		FeePct         *float64  `json:"fee_pct"`
		From           *string   `json:"from"`
		To             *string   `json:"to"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("arguments invalides : %v", err), true
	}
	base, err := a.db.GetStrategy(in.StrategyID)
	if err != nil || base == nil {
		return fmt.Sprintf("stratégie #%d introuvable : %v", in.StrategyID, err), true
	}

	// Un axe vide -> une seule valeur, celle de la stratégie de base.
	rsiTFs := in.RSITimeframes
	if len(rsiTFs) == 0 {
		rsiTFs = []string{base.RSITimeframe}
	}
	thresholds := in.RSIThresholds
	if len(thresholds) == 0 {
		th := 45.0
		if base.RSIThreshold != nil {
			th = *base.RSIThreshold
		}
		thresholds = []float64{th}
	}
	profits := in.ProfitTargets
	if len(profits) == 0 {
		profits = []float64{base.ProfitTarget}
	}
	intervals := in.BuyIntervals
	if len(intervals) == 0 {
		intervals = []int{base.BuyIntervalSeconds}
	}

	total := len(rsiTFs) * len(thresholds) * len(profits) * len(intervals)
	if total > 24 {
		return fmt.Sprintf("grille trop large (%d combinaisons, max 24) : réduis le nombre de valeurs par axe", total), true
	}

	priceTF := "15m"
	if in.PriceTimeframe != "" {
		priceTF = in.PriceTimeframe
	}
	feeRate := 0.001
	if in.FeePct != nil {
		feeRate = *in.FeePct / 100.0
	}
	candlesByTF, err := a.loadCandles()
	if err != nil {
		return err.Error(), true
	}
	if len(candlesByTF[priceTF]) == 0 {
		return fmt.Sprintf("aucune bougie %s/%s en base", a.pair, priceTF), true
	}
	startMs, endMs := parseDay(in.From, false), parseDay(in.To, true)

	// Résultat en TABLE MARKDOWN : bien plus compact en tokens qu'un tableau JSON
	// pour N variantes homogènes, et Claude le lit parfaitement.
	type row struct {
		line string
		net  float64
	}
	var rows []row
	for _, tf := range rsiTFs {
		for _, th := range thresholds {
			for _, pf := range profits {
				for _, itv := range intervals {
					s := *base
					s.RSITimeframe = tf
					thr := th
					s.RSIThreshold = &thr
					s.ProfitTarget = pf
					s.BuyIntervalSeconds = itv
					s.CronExpression = ""
					res, err := backtest.Run(backtest.Config{
						Pair: a.pair, Strategy: s, PriceTimeframe: priceTF,
						FeeRate: feeRate, Precision: precision, StartMs: startMs, EndMs: endMs,
					}, candlesByTF)
					if err != nil {
						return fmt.Sprintf("échec backtest (%s/%.0f/%.2f/%ds) : %v", tf, th, pf, itv, err), true
					}
					net := res.RealizedPnL + res.UnrealizedPnL
					rows = append(rows, row{
						line: fmt.Sprintf("| %s | %.0f | %.2f | %d | %.2f | %+.1f | %+.1f | %+.1f | %.0f | %.0f |",
							tf, th, pf, itv/3600, res.CyclesPerDay, res.RealizedPnL, res.UnrealizedPnL, net, res.PeakCapital, res.WinRate),
						net: net,
					})
				}
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].net > rows[j].net })

	var b strings.Builder
	fmt.Fprintf(&b, "Sweep %s — chemin de prix %s — %d combinaisons — trié par PnL net (réalisé+latent) décroissant.\n\n", a.pair, priceTF, total)
	b.WriteString("| rsi_tf | seuil | profit% | interval_h | cycles/j | pnl_réalisé | pnl_latent | net | capital_pic | win% |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		b.WriteString(r.line)
		b.WriteByte('\n')
	}
	return b.String(), false
}

func (a *Agent) toolRunBacktest(rawInput string) (string, bool) {
	var in struct {
		StrategyID          int      `json:"strategy_id"`
		RSITimeframe        *string  `json:"rsi_timeframe"`
		RSIThreshold        *float64 `json:"rsi_threshold"`
		ProfitTarget        *float64 `json:"profit_target"`
		BuyIntervalSeconds  *int     `json:"buy_interval_seconds"`
		QuoteAmount         *float64 `json:"quote_amount"`
		MaxConcurrentCycles *int     `json:"max_concurrent_cycles"`
		PriceTimeframe      *string  `json:"price_timeframe"`
		FeePct              *float64 `json:"fee_pct"`
		From                *string  `json:"from"`
		To                  *string  `json:"to"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("arguments invalides : %v", err), true
	}

	base, err := a.db.GetStrategy(in.StrategyID)
	if err != nil || base == nil {
		return fmt.Sprintf("stratégie #%d introuvable : %v", in.StrategyID, err), true
	}
	s := *base

	// Surcharges (uniquement les champs fournis par Claude).
	if in.RSITimeframe != nil {
		s.RSITimeframe = *in.RSITimeframe
	}
	if in.RSIThreshold != nil {
		s.RSIThreshold = in.RSIThreshold
	}
	if in.ProfitTarget != nil {
		s.ProfitTarget = *in.ProfitTarget
	}
	if in.BuyIntervalSeconds != nil {
		s.BuyIntervalSeconds = *in.BuyIntervalSeconds
	}
	if in.QuoteAmount != nil {
		s.QuoteAmount = *in.QuoteAmount
	}
	if in.MaxConcurrentCycles != nil {
		s.MaxConcurrentCycles = *in.MaxConcurrentCycles
	}
	// Le backtest est piloté par intervalle : on neutralise le cron pour éviter
	// toute ambiguïté (comme la commande backtest).
	s.CronExpression = ""

	priceTF := "15m"
	if in.PriceTimeframe != nil && *in.PriceTimeframe != "" {
		priceTF = *in.PriceTimeframe
	}
	feeRate := 0.001
	if in.FeePct != nil {
		feeRate = *in.FeePct / 100.0
	}

	candlesByTF, err := a.loadCandles()
	if err != nil {
		return err.Error(), true
	}
	if len(candlesByTF[priceTF]) == 0 {
		return fmt.Sprintf("aucune bougie %s/%s en base : impossible de simuler ce chemin de prix", a.pair, priceTF), true
	}

	cfg := backtest.Config{
		Pair:           a.pair,
		Strategy:       s,
		PriceTimeframe: priceTF,
		FeeRate:        feeRate,
		Precision:      precision,
		StartMs:        parseDay(in.From, false),
		EndMs:          parseDay(in.To, true),
	}
	res, err := backtest.Run(cfg, candlesByTF)
	if err != nil {
		return fmt.Sprintf("échec du backtest : %v", err), true
	}
	return jsonResult(res)
}

// parseDay convertit "YYYY-MM-DD" en millisecondes epoch (heure locale). Chaîne
// vide ou nil -> 0 (borne ouverte). endOfDay place le curseur à la fin de la
// journée.
func parseDay(s *string, endOfDay bool) int64 {
	if s == nil || *s == "" {
		return 0
	}
	t, err := time.ParseInLocation("2006-01-02", *s, time.Local)
	if err != nil {
		return 0
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Millisecond)
	}
	return t.UnixMilli()
}
