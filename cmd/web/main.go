package main

import (
	"bot/internal/loader"
	"bot/internal/logger"
	"bot/internal/version"
	"bot/internal/web"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Printf("Starting Simple Bot Web %s", version.Version)

	var (
		botDir = flag.String("root", ".", "Répertoire racine de l'instance du bot")
		port   = flag.String("port", "", "Port pour l'interface web (prioritaire sur la variable WEB_PORT)")
	)
	flag.Parse()

	rootDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Impossible de déterminer le répertoire courant : %v", err)
	}

	if *botDir != "." {
		if err := os.Chdir(*botDir); err != nil {
			log.Fatalf("Impossible de changer de répertoire vers %s : %v", *botDir, err)
		}
	}

	cfg, db, err := loader.LoadConfig()
	if err != nil {
		log.Fatalf("Échec du chargement de la configuration : %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Fatalf("Échec de la fermeture de la DB : %v", err)
		}
	}()

	effectivePort := cfg.WebPort
	if *port != "" {
		if intPort, err := strconv.Atoi(*port); err == nil {
			effectivePort = fmt.Sprintf(":%d", intPort)
		} else {
			effectivePort = *port
		}
	}

	router := web.SetupServer(cfg.ExchangeName, db, rootDir)
	router.Run(effectivePort)
}
