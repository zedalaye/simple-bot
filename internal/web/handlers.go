package web

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
)

// Variable globale pour le client bot
var botClient *BotClient

// Fonctions helper pour les templates
var templateFuncs = template.FuncMap{
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
}

func createRenderer() multitemplate.Renderer {
	r := multitemplate.NewRenderer()
	layoutPath := "templates/_shared/layout.html"

	// Find all .html files under templates/, excluding layout.html
	viewFiles, err := filepath.Glob("templates/**/*.html")
	if err != nil {
		log.Fatalf("Failed to glob template files: %v", err)
	}

	for _, viewFile := range viewFiles {
		if viewFile == layoutPath {
			continue // Skip the layout file itself
		}

		// Generate template name from file path (e.g., templates/orders/index.html -> orders_index)
		relPath, err := filepath.Rel("templates/", viewFile)
		if err != nil {
			log.Printf("Failed to get relative path for %s: %v", viewFile, err)
			continue
		}
		// Replace slashes with underscores and remove .html extension
		templateName := strings.ReplaceAll(strings.TrimSuffix(relPath, ".html"), "/", "_")
		log.Printf("Registering template: %s for file %s", templateName, viewFile)

		// Pair the view with the layout
		r.AddFromFilesFuncs(templateName, templateFuncs, layoutPath, viewFile)
	}

	return r
}

func makeTitle(exchangeName string, title string) string {
	return fmt.Sprintf("%s - %s - Simple Bot by PrY", exchangeName, title)
}

func registerHandlers(router *gin.Engine, exchangeName string, db *database.DB, client *BotClient) {
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
	router.GET("/orders", func(c *gin.Context) {
		orders, err := db.GetPendingOrders()
		if err != nil {
			handleError(c, "Erreur - Ordres", "orders", "Failed to get pending orders: "+err.Error())
			return
		}

		c.HTML(http.StatusOK, "orders_index", gin.H{
			"title":      makeTitle(exchangeName, "Ordres En Attente"),
			"exchange":   exchangeName,
			"active":     "orders",
			"pageTitle":  "Ordres En Attente",
			"orders":     orders,
			"orderType":  "pending",
			"currentURL": "/orders",
		})
	})

	// Tous les ordres
	router.GET("/orders/all", func(c *gin.Context) {
		orders, err := db.GetAllOrders()
		if err != nil {
			handleError(c, "Erreur - Ordres", "orders", "Failed to get all orders: "+err.Error())
			return
		}

		c.HTML(http.StatusOK, "orders_index", gin.H{
			"title":      makeTitle(exchangeName, "Tous les Ordres"),
			"exchange":   exchangeName,
			"active":     "orders",
			"pageTitle":  "Tous les Ordres",
			"orders":     orders,
			"orderType":  "all",
			"currentURL": "/orders/all",
		})
	})

	// Cycles
	router.GET("/cycles", func(c *gin.Context) {
		cycles, err := db.GetAllCycles()
		if err != nil {
			handleError(c, "Erreur - Cycles", "cycles", "Failed to get cycles: "+err.Error())
			return
		}

		c.HTML(http.StatusOK, "cycles_index", gin.H{
			"title":    makeTitle(exchangeName, "Cycles"),
			"exchange": exchangeName,
			"active":   "cycles",
			"cycles":   cycles,
		})
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
			"title":      makeTitle(exchangeName, "Nouvelle Stratégie"),
			"exchange":   exchangeName,
			"active":     "strategies",
			"algorithms": []string{"rsi_dca", "macd_cross"}, // Available algorithms
		})
	})

	// Create strategy (POST)
	router.POST("/strategies", func(c *gin.Context) {
		name := c.PostForm("name")
		description := c.PostForm("description")
		algorithm := c.PostForm("algorithm")
		cron := c.PostForm("cron")
		enabled := c.PostForm("enabled") == "on"

		quoteAmount, _ := strconv.ParseFloat(c.PostForm("quote_amount"), 64)
		profitTarget, _ := strconv.ParseFloat(c.PostForm("profit_target"), 64)
		trailingStopDelta, _ := strconv.ParseFloat(c.PostForm("trailing_stop_delta"), 64)
		sellOffset, _ := strconv.ParseFloat(c.PostForm("sell_offset"), 64)
		concurrentCycles, _ := strconv.ParseInt(c.PostForm("concurrent_cycles"), 10, 64)

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

		// Use the new comprehensive method
		err := db.CreateStrategyFromWeb(name, description, algorithm, cron, enabled,
			quoteAmount, profitTarget, trailingStopDelta, sellOffset,
			rsiThreshold, rsiPeriod, rsiTimeframe,
			macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
			bbPeriod, bbMultiplier, bbTimeframe,
			volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
			int(concurrentCycles))
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
			"title":      makeTitle(exchangeName, "Modifier Stratégie"),
			"exchange":   exchangeName,
			"active":     "strategies",
			"strategy":   strategy,
			"algorithms": []string{"rsi_dca", "macd_cross"},
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
		cron := c.PostForm("cron")
		enabled := c.PostForm("enabled") == "on"

		quoteAmount, _ := strconv.ParseFloat(c.PostForm("quote_amount"), 64)
		profitTarget, _ := strconv.ParseFloat(c.PostForm("profit_target"), 64)
		trailingStopDelta, _ := strconv.ParseFloat(c.PostForm("trailing_stop_delta"), 64)
		sellOffset, _ := strconv.ParseFloat(c.PostForm("sell_offset"), 64)
		concurrentCycles, _ := strconv.ParseInt(c.PostForm("concurrent_cycles"), 10, 64)

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

		err = db.UpdateStrategy(strategyID, name, description, algorithm, cron, enabled,
			quoteAmount, profitTarget, trailingStopDelta, sellOffset,
			rsiThreshold, rsiPeriod, rsiTimeframe,
			macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
			bbPeriod, bbMultiplier, bbTimeframe,
			volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
			int(concurrentCycles))
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

	// API endpoints JSON
	api := router.Group("/api")
	{
		api.GET("/stats", func(c *gin.Context) {
			metrics, err := db.GetDashboardMetrics()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, metrics)
		})

		api.GET("/orders", func(c *gin.Context) {
			orders, err := db.GetPendingOrders()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, orders)
		})

		api.GET("/orders/all", func(c *gin.Context) {
			orders, err := db.GetAllOrders()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, orders)
		})

		api.GET("/cycles", func(c *gin.Context) {
			cycles, err := db.GetAllCycles()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, cycles)
		})

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

			// Check if we need to update data based on last candle timestamp
			lastCandle, err := db.GetLastCandle(pair, timeframe)
			if err != nil {
				// If error getting last candle, continue with what we have
				logger.Warnf("Failed to get last candle for update check: %v", err)
			}

			var since *int64
			if lastCandle == nil {
				// No candles at all - definitely need to collect
				logger.Infof("No candles found for %s %s - will collect initial data", pair, timeframe)
			} else {
				sinceTime := lastCandle.Timestamp + 1
				since = &sinceTime
			}

			if botClient != nil {
				// Request collection from bot (MarketCollector will only fetch what's missing)
				_, err := botClient.RequestCandleCollection(pair, timeframe, since, limit)
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
func SetupServer(exchangeName string, db *database.DB) *gin.Engine {
	// Mode release pour la production
	// gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// Middlewares
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(securityHeaders())

	router.HTMLRender = createRenderer()

	// Servir les fichiers statiques si nécessaire
	router.Static("/static", "./static")

	// Initialiser le client bot
	client := NewBotClient()

	// Enregistrer les handlers
	registerHandlers(router, exchangeName, db, client)

	return router
}
