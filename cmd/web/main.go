package main

import (
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/web"
	"flag"
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	configFile := flag.String("config", "config.yml", "Path to configuration file (YAML format)")
	flag.Parse()

	// Load configuration
	fileConfig, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize database
	db, err := database.NewDB(fileConfig.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer func(db *database.DB) {
		err := db.Close()
		if err != nil {
			log.Fatalf("Failed to close database: %v", err)
		}
	}(db)

	// Initialize Gin router
	router := gin.Default()

	// Load templates
	router.LoadHTMLGlob("templates/*")

	// Register handlers
	web.RegisterHandlers(router, db)

	// Start server
	log.Println("Starting Web UI on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
