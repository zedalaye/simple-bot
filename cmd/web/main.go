package main

import (
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/web"
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	projectRoot, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)

	// Paramètres de ligne de commande
	var (
		botDir = flag.String("root", ".", "Path to the bot directory")
	)
	flag.Parse()

	// Changer le répertoire de travail si nécessaire
	if *botDir != "." {
		err := os.Chdir(*botDir)
		if err != nil {
			log.Fatalf("Failed to change directory to %s: %v", *botDir, err)
		}
	}

	// Load configuration
	fileConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	err = logger.InitLogger(fileConfig.GetLogLevel(), fileConfig.GetLogFile())
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Initialize database
	db, err := database.NewDB(fmt.Sprintf("file:%s?mode=ro", fileConfig.Database.Path))
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer func(db *database.DB) {
		err := db.Close()
		if err != nil {
			logger.Fatalf("Failed to close database: %v", err)
		}
	}(db)

	// Retourne au dossier racine par défaut
	err = os.Chdir(projectRoot)
	if err != nil {
		log.Fatalf("Failed to change directory back to %s: %v", projectRoot, err)
	}

	router := web.SetupServer(fileConfig.Exchange.Name, db)
	router.Run(":8080")
}
