package web

import (
	"bot/internal/core/database"
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

func registerHandlers(router *gin.Engine, exchangeName string, db *database.DB) {
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
			"autoRefresh": true,
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

		// Use the new comprehensive method instead of CreateExampleStrategy
		err := db.CreateStrategyFromWeb(name, description, algorithm, cron, enabled,
			quoteAmount, profitTarget, trailingStopDelta, sellOffset, rsiThreshold, rsiPeriod)
		if err != nil {
			handleError(c, "Erreur - Création Stratégie", "strategies", "Failed to create strategy: "+err.Error())
			return
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

		// Prevent deletion of Legacy Strategy (ID=1)
		if strategyID == 1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete Legacy Strategy"})
			return
		}

		// Delete strategy
		err = db.DeleteStrategy(strategyID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
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

	// Enregistrer les handlers
	registerHandlers(router, exchangeName, db)

	return router
}
