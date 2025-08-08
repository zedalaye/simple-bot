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
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{
				"error": "Failed to get stats",
			})
			return
		}

		c.HTML(http.StatusOK, "index.html", gin.H{
			"stats": stats,
		})
	})

	router.GET("/positions", func(c *gin.Context) {
		positions, err := db.GetAllPositions()
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{
				"error": "Failed to get positions",
			})
			return
		}

		c.HTML(http.StatusOK, "positions.html", gin.H{
			"positions": positions,
		})
	})

	router.GET("/orders", func(c *gin.Context) {
		orders, err := db.GetPendingOrders()
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{
				"error": "Failed to get orders",
			})
			return
		}

		c.HTML(http.StatusOK, "orders.html", gin.H{
			"orders": orders,
		})
	})
}
