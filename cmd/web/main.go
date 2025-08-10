package main

import (
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/web"
	"flag"
	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"log"
	"path/filepath"
	"strings"
)

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
		r.AddFromFiles(templateName, layoutPath, viewFile)
	}

	return r
}

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

	// Load all templates
	router.HTMLRender = createRenderer()

	// Serve static files if needed
	router.Static("/static", "./static")

	// Register handlers
	web.RegisterHandlers(router, db)

	// Start server
	log.Println("Starting Web UI on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
