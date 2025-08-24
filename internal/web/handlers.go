package web

import (
	"bot/internal/core/database"
	"fmt"
	"github.com/gin-contrib/multitemplate"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

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

func registerHandlers(router *gin.Engine, db *database.DB) {
	// Configuration des templates

	// router.LoadHTMLGlob("templates/*")

	// Page d'erreur générique
	handleError := func(c *gin.Context, title, active, errMsg string) {
		c.HTML(http.StatusInternalServerError, "error_index", gin.H{
			"title":  title,
			"active": active,
			"error":  errMsg,
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
			"title":       "Dashboard - Trading Bot",
			"active":      "dashboard",
			"stats":       metrics,
			"avgProfit":   metrics["avg_profit"],
			"totalProfit": metrics["total_profit"],
			"successRate": metrics["success_rate"],
			"autoRefresh": true,
		})
	})

	// Positions
	router.GET("/positions", func(c *gin.Context) {
		positions, err := db.GetAllPositions()
		if err != nil {
			handleError(c, "Erreur - Positions", "positions", "Failed to get positions: "+err.Error())
			return
		}

		// Calculer la valeur pour chaque position et le total
		type PositionWithValue struct {
			database.Position
			Value float64
		}

		positionsWithValue := make([]PositionWithValue, len(positions))
		totalValue := 0.0

		for i, pos := range positions {
			value := pos.Price * pos.Amount
			positionsWithValue[i] = PositionWithValue{
				Position: pos,
				Value:    value,
			}
			totalValue += value
		}

		c.HTML(http.StatusOK, "positions_index", gin.H{
			"title":      "Positions - Trading Bot",
			"active":     "positions",
			"positions":  positionsWithValue,
			"totalValue": totalValue,
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
			"title":      "Ordres En Attente - Trading Bot",
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
			"title":      "Tous les Ordres - Trading Bot",
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
			"title":  "Cycles - Trading Bot",
			"active": "cycles",
			"cycles": cycles,
		})
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

		api.GET("/positions", func(c *gin.Context) {
			positions, err := db.GetAllPositions()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, positions)
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
func SetupServer(db *database.DB, port string) *gin.Engine {
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
	registerHandlers(router, db)

	return router
}
