package web

import (
	"bot/internal/core/database"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterHandlers(router *gin.Engine, db *database.DB) {
	router.GET("/", func(c *gin.Context) {
		stats, err := db.GetStats()
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error_index", gin.H{
				"title": "Error",
				"error": "Failed to get stats: " + err.Error(),
			})
			return
		}

		c.HTML(http.StatusOK, "dashboard_index", gin.H{
			"title": "Dashboard - Statistics",
			"stats": stats,
		})
	})

	router.GET("/positions", func(c *gin.Context) {
		positions, err := db.GetAllPositions()
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error_index", gin.H{
				"title": "Error",
				"error": "Failed to get positions: " + err.Error(),
			})
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
			"title":      "Open Positions",
			"positions":  positionsWithValue,
			"totalValue": totalValue,
		})
	})

	router.GET("/orders", func(c *gin.Context) {
		orders, err := db.GetPendingOrders()
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error_index", gin.H{
				"title": "Error",
				"error": "Failed to get pending orders: " + err.Error(),
			})
			return
		}

		c.HTML(http.StatusOK, "orders_index", gin.H{
			"title":  "Pending Orders",
			"orders": orders,
		})
	})

	// Route pour voir tous les ordres (pas seulement pending)
	router.GET("/orders/all", func(c *gin.Context) {
		orders, err := db.GetAllOrders()
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error_index", gin.H{
				"title": "Error",
				"error": "Failed to get all orders: " + err.Error(),
			})
			return
		}

		c.HTML(http.StatusOK, "orders_all", gin.H{
			"title":  "All Orders",
			"orders": orders,
		})
	})

	// API endpoints JSON
	api := router.Group("/api")
	{
		api.GET("/stats", func(c *gin.Context) {
			stats, err := db.GetStats()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, stats)
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
	}
}
