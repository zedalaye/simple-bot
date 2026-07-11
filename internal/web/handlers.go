package web

import (
	"bot/internal/algorithms"
	"bot/internal/chat"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"
	"bot/internal/version"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

// Variable globale pour le client bot
var botClient *BotClient

// Registre d'algorithmes pour valider les configurations de stratégie côté serveur,
// avec exactement les mêmes règles que celles appliquées au runtime par le scheduler.
var strategyRegistry = algorithms.NewAlgorithmRegistry()

// validateStrategyForm valide une stratégie soumise via le formulaire web. Couvre
// d'abord les champs communs, le filtre de tendance EMA (non couvert par les algos),
// puis délègue la validation spécifique à l'algorithme (RSI/MACD + taille dynamique)
// à son ValidateConfig pour garantir la parité avec le runtime.
func validateStrategyForm(s database.Strategy) error {
	// Champs de base communs à toutes les stratégies
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("le nom de la stratégie est requis")
	}
	// Déclenchement des achats : cron (heure fixe) XOR intervalle périodique.
	// Exactement un des deux doit être renseigné.
	hasCron := strings.TrimSpace(s.CronExpression) != ""
	hasInterval := s.BuyIntervalSeconds > 0
	if hasCron && hasInterval {
		return fmt.Errorf("choisissez soit une expression cron, soit un intervalle périodique, pas les deux")
	}
	if !hasCron && !hasInterval {
		return fmt.Errorf("une expression cron ou un intervalle d'achat périodique est requis")
	}
	if hasCron {
		// Parité avec le runtime : le scheduler utilise robfig/cron pour planifier.
		// Un cron invalide passait la validation puis échouait silencieusement à se planifier.
		if _, err := cron.ParseStandard(strings.TrimSpace(s.CronExpression)); err != nil {
			return fmt.Errorf("expression cron invalide : %w", err)
		}
	}
	if hasInterval && s.BuyIntervalSeconds < 3600 {
		return fmt.Errorf("l'intervalle d'achat périodique doit être d'au moins 1 heure (reçu %d s)", s.BuyIntervalSeconds)
	}
	if s.QuoteAmount <= 0 {
		return fmt.Errorf("le montant par ordre doit être positif (reçu %.2f)", s.QuoteAmount)
	}
	if s.MaxConcurrentCycles < 0 {
		return fmt.Errorf("le nombre de cycles simultanés ne peut pas être négatif (0 = illimité, reçu %d)", s.MaxConcurrentCycles)
	}
	if s.MaxBuyOrderAgeHours < 0 {
		return fmt.Errorf("l'âge maximal d'un ordre d'achat ne peut pas être négatif (0 = désactivé, reçu %d)", s.MaxBuyOrderAgeHours)
	}
	if s.ProfitTarget <= 0 {
		return fmt.Errorf("l'objectif de profit doit être positif (reçu %.2f)", s.ProfitTarget)
	}
	if s.TrailingStopDelta < 0 {
		return fmt.Errorf("le trailing stop ne peut pas être négatif (reçu %.2f)", s.TrailingStopDelta)
	}
	if s.SellOffset < 0 {
		return fmt.Errorf("l'offset de vente ne peut pas être négatif (reçu %.2f)", s.SellOffset)
	}
	if s.VolatilityPeriod != nil && *s.VolatilityPeriod <= 0 {
		return fmt.Errorf("la période de volatilité doit être positive (reçu %d)", *s.VolatilityPeriod)
	}

	// Filtre de tendance EMA (si activé) — non couvert par les ValidateConfig des algos
	if s.TrendFilterEnabled {
		if s.TrendFilterFastPeriod == nil || s.TrendFilterSlowPeriod == nil {
			return fmt.Errorf("les périodes EMA rapide et lente sont requises quand le filtre de tendance est activé")
		}
		if *s.TrendFilterFastPeriod <= 0 || *s.TrendFilterSlowPeriod <= 0 {
			return fmt.Errorf("les périodes EMA du filtre de tendance doivent être positives")
		}
		if *s.TrendFilterFastPeriod >= *s.TrendFilterSlowPeriod {
			return fmt.Errorf("l'EMA rapide (%d) doit être inférieure à l'EMA lente (%d)",
				*s.TrendFilterFastPeriod, *s.TrendFilterSlowPeriod)
		}
	}

	// Validation spécifique à l'algorithme (réutilise les règles du runtime)
	algo, ok := strategyRegistry.Get(s.AlgorithmName)
	if !ok {
		return fmt.Errorf("algorithme inconnu : %q", s.AlgorithmName)
	}
	return algo.ValidateConfig(s)
}

// parseTrigger lit le mode de déclenchement des achats depuis le formulaire :
// soit une expression cron (heure fixe), soit un intervalle périodique
// (valeur + unité). Retourne (cron, intervalSeconds) avec au plus un renseigné.
func parseTrigger(c *gin.Context) (string, int) {
	if c.PostForm("trigger_mode") == "interval" {
		value, _ := strconv.Atoi(c.PostForm("buy_interval_value"))
		if value <= 0 {
			return "", 0
		}
		unitSeconds := 3600 // heures par défaut
		switch c.PostForm("buy_interval_unit") {
		case "days":
			unitSeconds = 86400
		case "weeks":
			unitSeconds = 604800
		}
		return "", value * unitSeconds
	}
	return strings.TrimSpace(c.PostForm("cron")), 0
}

// parseBuyOrderAge lit l'âge maximal d'un ordre d'achat (valeur + unité) depuis le
// formulaire et le convertit en heures. 0 = désactivé.
func parseBuyOrderAge(c *gin.Context) int {
	value, _ := strconv.Atoi(c.PostForm("max_buy_order_age_value"))
	if value <= 0 {
		return 0
	}
	unitHours := 1 // heures par défaut
	switch c.PostForm("max_buy_order_age_unit") {
	case "days":
		unitHours = 24
	case "weeks":
		unitHours = 168
	}
	return value * unitHours
}

// Fonctions helper pour les templates
var templateFuncs = template.FuncMap{
	// version : exposée à tous les templates (via le layout partagé) pour afficher
	// la version du binaire (injectée par make release) dans le footer.
	"version": func() string { return version.Version },
	"timeAgo": func(t time.Time) string {
		duration := time.Since(t)
		if duration < time.Minute {
			return "À l'instant"
		} else if duration < time.Hour {
			minutes := int(duration.Minutes())
			return fmt.Sprintf("Il y a %d min", minutes)
		} else if duration < 24*time.Hour {
			hours := int(duration.Hours())
			return fmt.Sprintf("Il y a %d h", hours)
		} else {
			days := int(duration.Hours() / 24)
			return fmt.Sprintf("Il y a %d j", days)
		}
	},
	// intervalUnit / intervalValue : décomposent un intervalle (en secondes) en
	// (valeur, unité) pour le round-trip du formulaire. Choisit la plus grande
	// unité qui divise exactement l'intervalle.
	"intervalUnit": func(seconds int) string {
		switch {
		case seconds > 0 && seconds%604800 == 0:
			return "weeks"
		case seconds > 0 && seconds%86400 == 0:
			return "days"
		default:
			return "hours"
		}
	},
	"intervalValue": func(seconds int) int {
		switch {
		case seconds > 0 && seconds%604800 == 0:
			return seconds / 604800
		case seconds > 0 && seconds%86400 == 0:
			return seconds / 86400
		default:
			return seconds / 3600
		}
	},
	// buyAgeUnit / buyAgeValue : décomposent un âge max d'ordre d'achat (stocké en
	// heures) en (valeur, unité) pour le round-trip du formulaire. Choisit la plus
	// grande unité qui divise exactement la durée.
	"buyAgeUnit": func(hours int) string {
		switch {
		case hours > 0 && hours%168 == 0:
			return "weeks"
		case hours > 0 && hours%24 == 0:
			return "days"
		default:
			return "hours"
		}
	},
	"buyAgeValue": func(hours int) int {
		switch {
		case hours > 0 && hours%168 == 0:
			return hours / 168
		case hours > 0 && hours%24 == 0:
			return hours / 24
		default:
			return hours
		}
	},
	// intervalLabel : libellé court FR d'un intervalle (ex: « toutes les 24 h »).
	"intervalLabel": func(seconds int) string {
		switch {
		case seconds > 0 && seconds%604800 == 0:
			return fmt.Sprintf("toutes les %d sem", seconds/604800)
		case seconds > 0 && seconds%86400 == 0:
			return fmt.Sprintf("tous les %d j", seconds/86400)
		default:
			return fmt.Sprintf("toutes les %d h", seconds/3600)
		}
	},
	"add": func(a, b float64) float64 {
		return a + b
	},
	"sub": func(a, b float64) float64 {
		return a - b
	},
	"mul": func(a, b float64) float64 {
		return a * b
	},
	"div": func(a, b float64) float64 {
		if b != 0 {
			return a / b
		}
		return 0
	},
	//"gt": func(a, b float64) bool {
	//	return a > b
	//},
	// now : instant courant, pour les durées « vivantes » (cycle non encore clôturé)
	// via formatDuration .Start now.
	"now": func() time.Time {
		return time.Now()
	},
	"formatDuration": func(start, end time.Time) string {
		duration := end.Sub(start)
		if duration < time.Minute {
			return fmt.Sprintf("%d sec", int(duration.Seconds()))
		} else if duration < time.Hour {
			return fmt.Sprintf("%d min", int(duration.Minutes()))
		} else if duration < 24*time.Hour {
			return fmt.Sprintf("%d h %d min", int(duration.Hours()), int(duration.Minutes())%60)
		} else {
			days := int(duration.Hours()) / 24
			hours := int(duration.Hours()) % 24
			return fmt.Sprintf("%d j %d h", days, hours)
		}
	},
	"derefFloat": func(f *float64) float64 {
		if f != nil {
			return *f
		}
		return 0
	},
	"derefInt": func(i *int) int {
		if i != nil {
			return *i
		}
		return 0
	},
}

func createRenderer(rootDir string) multitemplate.Renderer {
	r := multitemplate.NewRenderer()
	layoutPath := filepath.Join(rootDir, "templates/_shared/layout.html")

	// Find all .html files under templates/, excluding layout.html
	viewFiles, err := filepath.Glob(filepath.Join(rootDir, "templates/**/*.html"))
	if err != nil {
		log.Fatalf("Failed to glob template files: %v", err)
	}

	// Séparer les partials (fichiers _*.html) des vues. Les partials ne sont pas
	// des pages autonomes : ils sont ajoutés au parse-set de chaque vue pour être
	// réutilisables via {{template "..."}}.
	var partials []string
	for _, viewFile := range viewFiles {
		if strings.HasPrefix(filepath.Base(viewFile), "_") {
			partials = append(partials, viewFile)
		}
	}

	for _, viewFile := range viewFiles {
		if viewFile == layoutPath {
			continue // Skip the layout file itself
		}
		if strings.HasPrefix(filepath.Base(viewFile), "_") {
			continue // Skip partials, registered alongside each view below
		}

		// Generate template name from file path (e.g., templates/orders/index.html -> orders_index)
		relPath, err := filepath.Rel(filepath.Join(rootDir, "templates"), viewFile)
		if err != nil {
			log.Printf("Failed to get relative path for %s: %v", viewFile, err)
			continue
		}
		// Replace slashes with underscores and remove .html extension
		templateName := strings.ReplaceAll(strings.TrimSuffix(relPath, ".html"), "/", "_")
		log.Printf("Registering template: %s for file %s", templateName, viewFile)

		// Pair the view with the layout and all shared partials
		files := append([]string{layoutPath, viewFile}, partials...)
		r.AddFromFilesFuncs(templateName, templateFuncs, files...)
	}

	return r
}

func makeTitle(exchangeName string, title string) string {
	return fmt.Sprintf("%s - %s - Simple Bot by PrY", exchangeName, title)
}

func registerHandlers(router *gin.Engine, exchangeName, tradingPair string, db *database.DB, client *BotClient, logFilePath string) {
	// Assigner le client bot global
	botClient = client
	// Configuration des templates

	// router.LoadHTMLGlob("templates/*")

	// Page d'erreur générique
	handleError := func(c *gin.Context, title, active, errMsg string) {
		c.HTML(http.StatusInternalServerError, "error_index", gin.H{
			"title":    makeTitle(exchangeName, title),
			"exchange": exchangeName,
			"active":   active,
			"error":    errMsg,
		})
	}

	// Dashboard
	router.GET("/", func(c *gin.Context) {
		metrics, err := db.GetDashboardMetrics()
		if err != nil {
			handleError(c, "Erreur - Dashboard", "dashboard", "Failed to get dashboard metrics: "+err.Error())
			return
		}

		c.HTML(http.StatusOK, "dashboard_index", gin.H{
			"title":       makeTitle(exchangeName, "Dashboard"),
			"exchange":    exchangeName,
			"active":      "dashboard",
			"stats":       metrics,
			"avgProfit":   metrics["avg_profit"],
			"totalProfit": metrics["total_profit"],
			"successRate": metrics["success_rate"],
			"autoRefresh": false,
		})
	})

	// Ordres en attente
	// Ordres - helper local pour éviter la répétition
	serveOrders := func(c *gin.Context, filter, pageTitle, currentURL string) {
		orders, err := db.GetOrders(filter)
		if err != nil {
			handleError(c, "Erreur - Ordres", "orders", "Failed to get orders: "+err.Error())
			return
		}
		c.HTML(http.StatusOK, "orders_index", gin.H{
			"title":      makeTitle(exchangeName, pageTitle),
			"exchange":   exchangeName,
			"active":     "orders",
			"pageTitle":  pageTitle,
			"orders":     orders,
			"orderType":  filter,
			"currentURL": currentURL,
		})
	}

	router.GET("/orders", func(c *gin.Context) {
		serveOrders(c, "pending", "Ordres En attente", "/orders")
	})
	router.GET("/orders/filled", func(c *gin.Context) {
		serveOrders(c, "filled", "Ordres Exécutés", "/orders/filled")
	})
	router.GET("/orders/cancelled", func(c *gin.Context) {
		serveOrders(c, "cancelled", "Ordres Annulés", "/orders/cancelled")
	})
	router.GET("/orders/all", func(c *gin.Context) {
		serveOrders(c, "all", "Tous les Ordres", "/orders/all")
	})

	// Cycles - helper local pour éviter la répétition
	serveCycles := func(c *gin.Context, filter, titleSuffix, currentURL string) {
		cycles, err := db.GetCycles(filter)
		if err != nil {
			handleError(c, "Erreur - Cycles", "cycles", "Failed to get cycles: "+err.Error())
			return
		}
		c.HTML(http.StatusOK, "cycles_index", gin.H{
			"title":      makeTitle(exchangeName, titleSuffix),
			"exchange":   exchangeName,
			"active":     "cycles",
			"cycles":     cycles,
			"cycleType":  filter,
			"currentURL": currentURL,
		})
	}

	router.GET("/cycles", func(c *gin.Context) {
		serveCycles(c, "active", "Cycles Actifs", "/cycles")
	})
	router.GET("/cycles/new", func(c *gin.Context) {
		serveCycles(c, "new", "Nouveaux Cycles", "/cycles/new")
	})
	router.GET("/cycles/cancelled", func(c *gin.Context) {
		serveCycles(c, "cancelled", "Cycles Annulés", "/cycles/cancelled")
	})
	router.GET("/cycles/open", func(c *gin.Context) {
		serveCycles(c, "open", "Cycles Ouverts", "/cycles/open")
	})
	router.GET("/cycles/running", func(c *gin.Context) {
		serveCycles(c, "running", "Cycles En Cours", "/cycles/running")
	})
	router.GET("/cycles/completed", func(c *gin.Context) {
		serveCycles(c, "completed", "Cycles Terminés", "/cycles/completed")
	})
	router.GET("/cycles/all", func(c *gin.Context) {
		serveCycles(c, "all", "Tous les Cycles", "/cycles/all")
	})

	// Strategies - NEW!
	router.GET("/strategies", func(c *gin.Context) {
		strategies, err := db.GetAllStrategies()
		if err != nil {
			handleError(c, "Erreur - Stratégies", "strategies", "Failed to get strategies: "+err.Error())
			return
		}

		c.HTML(http.StatusOK, "strategies_index", gin.H{
			"title":      makeTitle(exchangeName, "Stratégies"),
			"exchange":   exchangeName,
			"active":     "strategies",
			"strategies": strategies,
		})
	})

	// Create new strategy form
	router.GET("/strategies/new", func(c *gin.Context) {
		c.HTML(http.StatusOK, "strategies_new", gin.H{
			"title":       makeTitle(exchangeName, "Nouvelle Stratégie"),
			"exchange":    exchangeName,
			"active":      "strategies",
			"algorithms":  []string{"rsi_dca", "macd_cross"},          // Available algorithms
			"strategy":    &database.Strategy{MaxConcurrentCycles: 1}, // Défauts explicites (1 cycle) ; 0 est réservé à « illimité »
			"pageTitle":   "Nouvelle Stratégie",
			"cardHeader":  "Configuration de la stratégie",
			"formAction":  "/strategies",
			"submitLabel": "Créer la stratégie",
			"submitClass": "btn-success",
		})
	})

	// Create strategy (POST)
	router.POST("/strategies", func(c *gin.Context) {
		name := c.PostForm("name")
		description := c.PostForm("description")
		algorithm := c.PostForm("algorithm")
		cron, buyIntervalSeconds := parseTrigger(c)
		enabled := c.PostForm("enabled") == "on"

		quoteAmount, _ := strconv.ParseFloat(c.PostForm("quote_amount"), 64)
		profitTarget, _ := strconv.ParseFloat(c.PostForm("profit_target"), 64)
		trailingStopDelta, _ := strconv.ParseFloat(c.PostForm("trailing_stop_delta"), 64)
		sellOffset, _ := strconv.ParseFloat(c.PostForm("sell_offset"), 64)
		concurrentCycles, _ := strconv.ParseInt(c.PostForm("concurrent_cycles"), 10, 64)
		maxBuyOrderAgeHours := parseBuyOrderAge(c)

		// Set defaults if not provided
		if trailingStopDelta == 0 {
			trailingStopDelta = 0.1
		}
		if sellOffset == 0 {
			sellOffset = 0.1
		}

		// RSI parameters
		var rsiThreshold *float64
		var rsiPeriod *int
		rsiTimeframe := c.PostForm("rsi_timeframe")
		if rsiThreshStr := c.PostForm("rsi_threshold"); rsiThreshStr != "" {
			if val, err := strconv.ParseFloat(rsiThreshStr, 64); err == nil {
				rsiThreshold = &val
			}
		}
		if rsiPeriodStr := c.PostForm("rsi_period"); rsiPeriodStr != "" {
			if val, err := strconv.Atoi(rsiPeriodStr); err == nil {
				rsiPeriod = &val
			}
		}

		// MACD parameters
		macdFastPeriod, _ := strconv.Atoi(c.PostForm("macd_fast_period"))
		macdSlowPeriod, _ := strconv.Atoi(c.PostForm("macd_slow_period"))
		macdSignalPeriod, _ := strconv.Atoi(c.PostForm("macd_signal_period"))
		macdTimeframe := c.PostForm("macd_timeframe")
		if macdFastPeriod == 0 {
			macdFastPeriod = 12
		}
		if macdSlowPeriod == 0 {
			macdSlowPeriod = 26
		}
		if macdSignalPeriod == 0 {
			macdSignalPeriod = 9
		}

		// Bollinger Bands parameters
		bbPeriod, _ := strconv.Atoi(c.PostForm("bb_period"))
		bbMultiplier, _ := strconv.ParseFloat(c.PostForm("bb_multiplier"), 64)
		bbTimeframe := c.PostForm("bb_timeframe")
		if bbPeriod == 0 {
			bbPeriod = 20
		}
		if bbMultiplier == 0 {
			bbMultiplier = 2.0
		}

		// Volatility parameters
		var volatilityPeriod *int
		var volatilityAdjustment *float64
		volatilityTimeframe := c.PostForm("volatility_timeframe")
		if volPeriodStr := c.PostForm("volatility_period"); volPeriodStr != "" {
			if val, err := strconv.Atoi(volPeriodStr); err == nil {
				volatilityPeriod = &val
			}
		}
		if volAdjStr := c.PostForm("volatility_adjustment"); volAdjStr != "" {
			if val, err := strconv.ParseFloat(volAdjStr, 64); err == nil {
				volatilityAdjustment = &val
			}
		}

		// Trend filter parameters
		trendFilterEnabled := c.PostForm("trend_filter_enabled") == "on"
		var trendFilterFastPeriod, trendFilterSlowPeriod *int
		trendFilterTimeframe := c.PostForm("trend_filter_timeframe")
		if val, err := strconv.Atoi(c.PostForm("trend_filter_fast_period")); err == nil && val > 0 {
			trendFilterFastPeriod = &val
		}
		if val, err := strconv.Atoi(c.PostForm("trend_filter_slow_period")); err == nil && val > 0 {
			trendFilterSlowPeriod = &val
		}

		// Taille dynamique (montant par ordre proportionnel à la profondeur de baisse)
		dynamicSizingEnabled := c.PostForm("dynamic_sizing_enabled") == "on"
		var dynamicSizingMin, dynamicSizingMax, dynamicSizingFullDrawdown *float64
		var dynamicSizingWindowDays *int
		if val, err := strconv.ParseFloat(c.PostForm("dynamic_sizing_min"), 64); err == nil && val > 0 {
			dynamicSizingMin = &val
		}
		if val, err := strconv.ParseFloat(c.PostForm("dynamic_sizing_max"), 64); err == nil && val > 0 {
			dynamicSizingMax = &val
		}
		if val, err := strconv.Atoi(c.PostForm("dynamic_sizing_window_days")); err == nil && val > 0 {
			dynamicSizingWindowDays = &val
		}
		if val, err := strconv.ParseFloat(c.PostForm("dynamic_sizing_full_drawdown"), 64); err == nil && val > 0 {
			dynamicSizingFullDrawdown = &val
		}

		// Valider la configuration avant insertion (parité avec le runtime)
		if err := validateStrategyForm(database.Strategy{
			Name: name, Description: description, Enabled: enabled,
			AlgorithmName: algorithm, CronExpression: cron, BuyIntervalSeconds: buyIntervalSeconds, QuoteAmount: quoteAmount,
			MaxConcurrentCycles: int(concurrentCycles), MaxBuyOrderAgeHours: maxBuyOrderAgeHours,
			ProfitTarget: profitTarget, TrailingStopDelta: trailingStopDelta, SellOffset: sellOffset,
			RSIThreshold: rsiThreshold, RSIPeriod: rsiPeriod, RSITimeframe: rsiTimeframe,
			MACDFastPeriod: macdFastPeriod, MACDSlowPeriod: macdSlowPeriod, MACDSignalPeriod: macdSignalPeriod, MACDTimeframe: macdTimeframe,
			BBPeriod: bbPeriod, BBMultiplier: bbMultiplier, BBTimeframe: bbTimeframe,
			VolatilityPeriod: volatilityPeriod, VolatilityAdjustment: volatilityAdjustment, VolatilityTimeframe: volatilityTimeframe,
			TrendFilterEnabled: trendFilterEnabled, TrendFilterFastPeriod: trendFilterFastPeriod, TrendFilterSlowPeriod: trendFilterSlowPeriod, TrendFilterTimeframe: trendFilterTimeframe,
			DynamicSizingEnabled: dynamicSizingEnabled, DynamicSizingMin: dynamicSizingMin, DynamicSizingMax: dynamicSizingMax,
			DynamicSizingWindowDays: dynamicSizingWindowDays, DynamicSizingFullDrawdown: dynamicSizingFullDrawdown,
		}); err != nil {
			handleError(c, "Erreur - Création Stratégie", "strategies", "Configuration invalide : "+err.Error())
			return
		}

		// Use the new comprehensive method
		err := db.CreateStrategyFromWeb(name, description, algorithm, cron, buyIntervalSeconds, enabled,
			quoteAmount, profitTarget, trailingStopDelta, sellOffset,
			rsiThreshold, rsiPeriod, rsiTimeframe,
			macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
			bbPeriod, bbMultiplier, bbTimeframe,
			volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
			trendFilterEnabled, trendFilterFastPeriod, trendFilterSlowPeriod, trendFilterTimeframe,
			dynamicSizingEnabled, dynamicSizingMin, dynamicSizingMax, dynamicSizingWindowDays, dynamicSizingFullDrawdown,
			int(concurrentCycles), maxBuyOrderAgeHours)
		if err != nil {
			handleError(c, "Erreur - Création Stratégie", "strategies", "Failed to create strategy: "+err.Error())
			return
		}

		// Notifier le bot après création de stratégie
		if err := botClient.NotifyReload(); err != nil {
			logger.Warnf("Failed to notify bot of strategy creation: %v", err)
		}

		c.Redirect(http.StatusFound, "/strategies")
	})

	// Edit strategy form
	router.GET("/strategies/:id/edit", func(c *gin.Context) {
		idStr := c.Param("id")
		strategyID, err := strconv.Atoi(idStr)
		if err != nil {
			handleError(c, "Erreur - Stratégies", "strategies", "Invalid strategy ID")
			return
		}

		strategy, err := db.GetStrategy(strategyID)
		if err != nil {
			handleError(c, "Erreur - Stratégies", "strategies", "Failed to get strategy: "+err.Error())
			return
		}

		c.HTML(http.StatusOK, "strategies_edit", gin.H{
			"title":       makeTitle(exchangeName, "Modifier Stratégie"),
			"exchange":    exchangeName,
			"active":      "strategies",
			"strategy":    strategy,
			"algorithms":  []string{"rsi_dca", "macd_cross"},
			"pageTitle":   "Modification de la Stratégie",
			"cardHeader":  "Édition de la stratégie",
			"formAction":  fmt.Sprintf("/strategies/%d/update", strategy.ID),
			"submitLabel": "Modifier la stratégie",
			"submitClass": "btn-primary",
		})
	})

	// Update strategy
	router.POST("/strategies/:id/update", func(c *gin.Context) {
		idStr := c.Param("id")
		strategyID, err := strconv.Atoi(idStr)
		if err != nil {
			handleError(c, "Erreur - Stratégies", "strategies", "Invalid strategy ID")
			return
		}

		// Extraire les données du formulaire (similaire à la création)
		name := c.PostForm("name")
		description := c.PostForm("description")
		algorithm := c.PostForm("algorithm")
		cron, buyIntervalSeconds := parseTrigger(c)
		enabled := c.PostForm("enabled") == "on"

		quoteAmount, _ := strconv.ParseFloat(c.PostForm("quote_amount"), 64)
		profitTarget, _ := strconv.ParseFloat(c.PostForm("profit_target"), 64)
		trailingStopDelta, _ := strconv.ParseFloat(c.PostForm("trailing_stop_delta"), 64)
		sellOffset, _ := strconv.ParseFloat(c.PostForm("sell_offset"), 64)
		concurrentCycles, _ := strconv.ParseInt(c.PostForm("concurrent_cycles"), 10, 64)
		maxBuyOrderAgeHours := parseBuyOrderAge(c)

		// Set defaults if not provided
		if trailingStopDelta == 0 {
			trailingStopDelta = 0.1
		}
		if sellOffset == 0 {
			sellOffset = 0.1
		}

		// RSI parameters
		var rsiThreshold *float64
		var rsiPeriod *int
		rsiTimeframe := c.PostForm("rsi_timeframe")
		if rsiThreshStr := c.PostForm("rsi_threshold"); rsiThreshStr != "" {
			if val, err := strconv.ParseFloat(rsiThreshStr, 64); err == nil {
				rsiThreshold = &val
			}
		}
		if rsiPeriodStr := c.PostForm("rsi_period"); rsiPeriodStr != "" {
			if val, err := strconv.Atoi(rsiPeriodStr); err == nil {
				rsiPeriod = &val
			}
		}

		// MACD parameters
		macdFastPeriod, _ := strconv.Atoi(c.PostForm("macd_fast_period"))
		macdSlowPeriod, _ := strconv.Atoi(c.PostForm("macd_slow_period"))
		macdSignalPeriod, _ := strconv.Atoi(c.PostForm("macd_signal_period"))
		macdTimeframe := c.PostForm("macd_timeframe")
		if macdFastPeriod == 0 {
			macdFastPeriod = 12
		}
		if macdSlowPeriod == 0 {
			macdSlowPeriod = 26
		}
		if macdSignalPeriod == 0 {
			macdSignalPeriod = 9
		}

		// Bollinger Bands parameters
		bbPeriod, _ := strconv.Atoi(c.PostForm("bb_period"))
		bbMultiplier, _ := strconv.ParseFloat(c.PostForm("bb_multiplier"), 64)
		bbTimeframe := c.PostForm("bb_timeframe")
		if bbPeriod == 0 {
			bbPeriod = 20
		}
		if bbMultiplier == 0 {
			bbMultiplier = 2.0
		}

		// Volatility parameters
		var volatilityPeriod *int
		var volatilityAdjustment *float64
		volatilityTimeframe := c.PostForm("volatility_timeframe")
		if volPeriodStr := c.PostForm("volatility_period"); volPeriodStr != "" {
			if val, err := strconv.Atoi(volPeriodStr); err == nil {
				volatilityPeriod = &val
			}
		}
		if volAdjStr := c.PostForm("volatility_adjustment"); volAdjStr != "" {
			if val, err := strconv.ParseFloat(volAdjStr, 64); err == nil {
				volatilityAdjustment = &val
			}
		}

		// Trend filter parameters
		trendFilterEnabled := c.PostForm("trend_filter_enabled") == "on"
		var trendFilterFastPeriod, trendFilterSlowPeriod *int
		trendFilterTimeframe := c.PostForm("trend_filter_timeframe")
		if val, err := strconv.Atoi(c.PostForm("trend_filter_fast_period")); err == nil && val > 0 {
			trendFilterFastPeriod = &val
		}
		if val, err := strconv.Atoi(c.PostForm("trend_filter_slow_period")); err == nil && val > 0 {
			trendFilterSlowPeriod = &val
		}

		// Taille dynamique (montant par ordre proportionnel à la profondeur de baisse)
		dynamicSizingEnabled := c.PostForm("dynamic_sizing_enabled") == "on"
		var dynamicSizingMin, dynamicSizingMax, dynamicSizingFullDrawdown *float64
		var dynamicSizingWindowDays *int
		if val, err := strconv.ParseFloat(c.PostForm("dynamic_sizing_min"), 64); err == nil && val > 0 {
			dynamicSizingMin = &val
		}
		if val, err := strconv.ParseFloat(c.PostForm("dynamic_sizing_max"), 64); err == nil && val > 0 {
			dynamicSizingMax = &val
		}
		if val, err := strconv.Atoi(c.PostForm("dynamic_sizing_window_days")); err == nil && val > 0 {
			dynamicSizingWindowDays = &val
		}
		if val, err := strconv.ParseFloat(c.PostForm("dynamic_sizing_full_drawdown"), 64); err == nil && val > 0 {
			dynamicSizingFullDrawdown = &val
		}

		// Valider la configuration avant mise à jour (parité avec le runtime)
		if err := validateStrategyForm(database.Strategy{
			Name: name, Description: description, Enabled: enabled,
			AlgorithmName: algorithm, CronExpression: cron, BuyIntervalSeconds: buyIntervalSeconds, QuoteAmount: quoteAmount,
			MaxConcurrentCycles: int(concurrentCycles), MaxBuyOrderAgeHours: maxBuyOrderAgeHours,
			ProfitTarget: profitTarget, TrailingStopDelta: trailingStopDelta, SellOffset: sellOffset,
			RSIThreshold: rsiThreshold, RSIPeriod: rsiPeriod, RSITimeframe: rsiTimeframe,
			MACDFastPeriod: macdFastPeriod, MACDSlowPeriod: macdSlowPeriod, MACDSignalPeriod: macdSignalPeriod, MACDTimeframe: macdTimeframe,
			BBPeriod: bbPeriod, BBMultiplier: bbMultiplier, BBTimeframe: bbTimeframe,
			VolatilityPeriod: volatilityPeriod, VolatilityAdjustment: volatilityAdjustment, VolatilityTimeframe: volatilityTimeframe,
			TrendFilterEnabled: trendFilterEnabled, TrendFilterFastPeriod: trendFilterFastPeriod, TrendFilterSlowPeriod: trendFilterSlowPeriod, TrendFilterTimeframe: trendFilterTimeframe,
			DynamicSizingEnabled: dynamicSizingEnabled, DynamicSizingMin: dynamicSizingMin, DynamicSizingMax: dynamicSizingMax,
			DynamicSizingWindowDays: dynamicSizingWindowDays, DynamicSizingFullDrawdown: dynamicSizingFullDrawdown,
		}); err != nil {
			handleError(c, "Erreur - Modification Stratégie", "strategies", "Configuration invalide : "+err.Error())
			return
		}

		err = db.UpdateStrategy(strategyID, name, description, algorithm, cron, buyIntervalSeconds, enabled,
			quoteAmount, profitTarget, trailingStopDelta, sellOffset,
			rsiThreshold, rsiPeriod, rsiTimeframe,
			macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
			bbPeriod, bbMultiplier, bbTimeframe,
			volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
			trendFilterEnabled, trendFilterFastPeriod, trendFilterSlowPeriod, trendFilterTimeframe,
			dynamicSizingEnabled, dynamicSizingMin, dynamicSizingMax, dynamicSizingWindowDays, dynamicSizingFullDrawdown,
			int(concurrentCycles), maxBuyOrderAgeHours)
		if err != nil {
			handleError(c, "Erreur - Modification Stratégie", "strategies", "Failed to update strategy: "+err.Error())
			return
		}

		// Notifier le bot après mise à jour de stratégie
		if err := botClient.NotifyReload(); err != nil {
			logger.Warnf("Failed to notify bot of strategy update: %v", err)
		}

		c.Redirect(http.StatusFound, "/strategies")
	})

	// Toggle strategy enabled/disabled
	router.POST("/strategies/:id/toggle", func(c *gin.Context) {
		idStr := c.Param("id")
		strategyID, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid strategy ID"})
			return
		}

		// Toggle strategy enabled status
		err = db.ToggleStrategyEnabled(strategyID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Notifier le bot après toggle de stratégie
		if err := botClient.NotifyReload(); err != nil {
			logger.Warnf("Failed to notify bot of strategy toggle: %v", err)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Strategy status toggled successfully"})
	})

	// Delete strategy
	router.DELETE("/strategies/:id", func(c *gin.Context) {
		idStr := c.Param("id")
		strategyID, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid strategy ID"})
			return
		}

		// Delete strategy
		err = db.DeleteStrategy(strategyID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Notifier le bot après suppression de stratégie
		if err := botClient.NotifyReload(); err != nil {
			logger.Warnf("Failed to notify bot of strategy deletion: %v", err)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Strategy deleted successfully"})
	})

	// Vue des logs (le webui lit le fichier LOG_FILE écrit par le bot, partagé via le volume db/)
	router.GET("/logs", func(c *gin.Context) {
		c.HTML(http.StatusOK, "logs_index", gin.H{
			"title":      makeTitle(exchangeName, "Logs"),
			"exchange":   exchangeName,
			"active":     "logs",
			"logFileSet": logFilePath != "",
		})
	})

	// Assistant IA : page de chat conseiller (analyse + backtest via Claude)
	router.GET("/chat", func(c *gin.Context) {
		c.HTML(http.StatusOK, "chat_index", gin.H{
			"title":     makeTitle(exchangeName, "Assistant"),
			"exchange":  exchangeName,
			"active":    "chat",
			"available": chat.Available(),
			"pair":      tradingPair,
		})
	})

	// Agent conseiller : construit une fois si la clé API est présente.
	var chatAgent *chat.Agent
	if chat.Available() {
		chatAgent = chat.NewAgent(db, exchangeName, tradingPair)
	}

	// API endpoints JSON
	api := router.Group("/api")
	{
		// Assistant IA : flux SSE de la boucle agentique. Le corps POST porte
		// l'historique de conversation ; la réponse streame les événements
		// (text / tool / tool_result / done / error).
		api.POST("/chat", func(c *gin.Context) {
			if chatAgent == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ANTHROPIC_API_KEY non configurée"})
				return
			}
			var body struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := c.BindJSON(&body); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if len(body.Messages) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "conversation vide"})
				return
			}

			// Historique navigateur (texte brut) -> messages typés de l'API.
			var history []anthropic.MessageParam
			for _, m := range body.Messages {
				block := anthropic.NewTextBlock(m.Content)
				if m.Role == "assistant" {
					history = append(history, anthropic.NewAssistantMessage(block))
				} else {
					history = append(history, anthropic.NewUserMessage(block))
				}
			}

			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Header("X-Accel-Buffering", "no")
			fmt.Fprint(c.Writer, ": ok\n\n")
			c.Writer.Flush()

			// emit sérialise chaque événement en SSE. On JSON-encode la donnée
			// pour qu'un texte multi-ligne tienne sur une seule ligne data:.
			emit := func(ev chat.Event) {
				data, _ := json.Marshal(ev.Data)
				fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", ev.Kind, data)
				c.Writer.Flush()
			}
			chatAgent.Stream(c.Request.Context(), history, emit)
		})

		// Logs : chargement initial des N dernières lignes
		api.GET("/logs", func(c *gin.Context) {
			if logFilePath == "" {
				c.JSON(http.StatusOK, gin.H{"configured": false, "lines": []string{}})
				return
			}
			n, _ := strconv.Atoi(c.DefaultQuery("lines", "500"))
			if n <= 0 || n > 5000 {
				n = 500
			}
			lines, err := tailLines(logFilePath, n)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"configured": true, "lines": lines})
		})

		// Logs : flux SSE temps réel des nouvelles lignes
		api.GET("/logs/stream", func(c *gin.Context) {
			if logFilePath == "" {
				c.JSON(http.StatusOK, gin.H{"configured": false})
				return
			}
			streamLogs(c, logFilePath)
		})

		api.GET("/stats", func(c *gin.Context) {
			metrics, err := db.GetDashboardMetrics()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, metrics)
		})

		// Achat manuel : relaie la demande au bot (override hors RSI/cooldown).
		api.POST("/buy", func(c *gin.Context) {
			if botClient == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bot client non configuré"})
				return
			}
			msg, err := botClient.TriggerBuy()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": msg})
		})

		serveAPIOrders := func(c *gin.Context, filter string) {
			orders, err := db.GetOrders(filter)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, orders)
		}

		api.GET("/orders", func(c *gin.Context) { serveAPIOrders(c, "pending") })
		api.GET("/orders/filled", func(c *gin.Context) { serveAPIOrders(c, "filled") })
		api.GET("/orders/cancelled", func(c *gin.Context) { serveAPIOrders(c, "cancelled") })
		api.GET("/orders/all", func(c *gin.Context) { serveAPIOrders(c, "all") })

		serveAPICycles := func(c *gin.Context, filter string) {
			cycles, err := db.GetCycles(filter)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, cycles)
		}

		api.GET("/cycles", func(c *gin.Context) { serveAPICycles(c, "all") })
		api.GET("/cycles/active", func(c *gin.Context) { serveAPICycles(c, "active") })
		api.GET("/cycles/new", func(c *gin.Context) { serveAPICycles(c, "new") })
		api.GET("/cycles/cancelled", func(c *gin.Context) { serveAPICycles(c, "cancelled") })
		api.GET("/cycles/open", func(c *gin.Context) { serveAPICycles(c, "open") })
		api.GET("/cycles/running", func(c *gin.Context) { serveAPICycles(c, "running") })
		api.GET("/cycles/completed", func(c *gin.Context) { serveAPICycles(c, "completed") })

		// Endpoint pour les profits détaillés
		api.GET("/profits", func(c *gin.Context) {
			avgProfit, totalProfit, err := db.GetProfitStats()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"avg_profit":   avgProfit,
				"total_profit": totalProfit,
			})
		})

		// Endpoint pour l'activité récente
		api.GET("/activity", func(c *gin.Context) {
			activity, err := db.GetRecentActivity(20)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, activity)
		})

		// Strategies API
		api.GET("/strategies", func(c *gin.Context) {
			strategies, err := db.GetAllStrategies()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, strategies)
		})

		api.GET("/pairs", func(c *gin.Context) {
			pairs, err := db.GetPairs()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, pairs)
		})

		// Candles API with intelligent data collection
		api.GET("/candles", func(c *gin.Context) {
			pair := c.Query("pair")
			timeframe := c.Query("timeframe")
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))

			if pair == "" || timeframe == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "pair and timeframe are required"})
				return
			}

			if botClient != nil {
				// Demander au bot de collecter les dernières bougies (toujours les plus récentes)
				_, err := botClient.RequestCandleCollection(pair, timeframe, nil, limit)
				if err != nil {
					logger.Warnf("Failed to collect candles from bot: %v", err)
					// Continue with existing data even if collection failed
				} else {
					logger.Infof("Successfully updated candles for %s %s", pair, timeframe)
				}
			}

			// Get candles from database (including any newly collected ones)
			candles, err := db.GetCandles(pair, timeframe, limit)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Format for TradingView
			var data []map[string]interface{}
			for _, candle := range candles {
				data = append(data, map[string]interface{}{
					"time":   candle.Timestamp / 1000,
					"open":   candle.OpenPrice,
					"high":   candle.HighPrice,
					"low":    candle.LowPrice,
					"close":  candle.ClosePrice,
					"volume": candle.Volume,
				})
			}

			c.JSON(http.StatusOK, data)
		})

		// RSI API : courbe RSI calculée sur la même timeframe que le graphe, pour
		// l'affichage dans un pane dédié. La période et le seuil par défaut sont
		// repris de la première stratégie rsi_dca activée (ils restent surchargeables
		// via query string) afin que la ligne de seuil reflète la config du bot.
		api.GET("/rsi", func(c *gin.Context) {
			pair := c.Query("pair")
			timeframe := c.Query("timeframe")
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))

			if pair == "" || timeframe == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "pair and timeframe are required"})
				return
			}

			// Valeurs par défaut depuis la stratégie rsi_dca activée.
			period := 14
			var threshold *float64
			if strategies, err := db.GetEnabledStrategies(); err == nil {
				for _, s := range strategies {
					if s.AlgorithmName == "rsi_dca" && s.RSIPeriod != nil {
						period = *s.RSIPeriod
						threshold = s.RSIThreshold
						break
					}
				}
			}
			if p, err := strconv.Atoi(c.Query("period")); err == nil && p > 0 {
				period = p
			}

			candles, err := db.GetCandles(pair, timeframe, limit)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			closes := make([]float64, len(candles))
			for i, candle := range candles {
				closes[i] = candle.ClosePrice
			}

			rsiValues, err := market.RSISeries(closes, period)
			if err != nil {
				// Pas assez de bougies : on renvoie une série vide plutôt qu'une erreur.
				c.JSON(http.StatusOK, gin.H{"period": period, "threshold": threshold, "data": []any{}})
				return
			}

			// RSISeries émet len(closes)-period valeurs : on aligne sur les dernières
			// bougies correspondantes (ordre chronologique).
			offset := len(candles) - len(rsiValues)
			data := make([]map[string]interface{}, 0, len(rsiValues))
			for i, v := range rsiValues {
				data = append(data, map[string]interface{}{
					"time":  candles[offset+i].Timestamp / 1000,
					"value": v,
				})
			}

			c.JSON(http.StatusOK, gin.H{"period": period, "threshold": threshold, "data": data})
		})

		// Balance portefeuille
		api.GET("/balance", func(c *gin.Context) {
			if botClient == nil {
				c.JSON(http.StatusOK, BalanceResponse{Balances: []BalanceEntry{}, TotalValue: 0})
				return
			}
			balance, err := botClient.FetchBalance()
			if err != nil {
				logger.Warnf("Failed to fetch balance from bot: %v", err)
				c.JSON(http.StatusOK, BalanceResponse{Balances: []BalanceEntry{}, TotalValue: 0})
				return
			}
			c.JSON(http.StatusOK, balance)
		})

		// Bot Status API
		api.GET("/bot/status", func(c *gin.Context) {
			if botClient == nil {
				c.JSON(http.StatusOK, gin.H{
					"status":    "unknown",
					"message":   "Bot client not configured",
					"timestamp": time.Now().Format(time.RFC3339),
				})
				return
			}

			err := botClient.CheckHealth()
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"status":    "offline",
					"message":   err.Error(),
					"timestamp": time.Now().Format(time.RFC3339),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status":    "online",
				"message":   "Bot is running normally",
				"timestamp": time.Now().Format(time.RFC3339),
			})
		})
	}
}

// Middleware pour ajouter les headers de sécurité
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// Fonction d'initialisation du serveur web
func SetupServer(exchangeName, tradingPair string, db *database.DB, rootDir, logFilePath string) *gin.Engine {
	// Mode release pour la production
	// gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// Middlewares
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(securityHeaders())

	router.HTMLRender = createRenderer(rootDir)

	// Servir les fichiers statiques si nécessaire
	router.Static("/static", filepath.Join(rootDir, "static"))

	// Initialiser le client bot
	client := NewBotClient()

	// Enregistrer les handlers
	registerHandlers(router, exchangeName, tradingPair, db, client, logFilePath)

	return router
}
