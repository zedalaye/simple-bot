// Package webcli implémente la sous-commande « web » : l'interface web + l'API REST.
package webcli

import (
	"bot/internal/cli"
	"bot/internal/loader"
	"bot/internal/logger"
	"bot/internal/version"
	"bot/internal/web"
	"flag"
	"fmt"
	"log"
	"strconv"
)

// Main est le point d'entrée de la sous-commande « web ». Le flag --root et le chdir
// sont gérés en amont par le dispatcher ; cli.RootWd fournit le cwd d'origine où
// vivent templates/ et static/ (racine du dépôt, pas l'instance).
func Main(args []string) {
	log.Printf("Starting Simple Bot Web %s", version.Version)

	port := flag.String("port", "", "Port pour l'interface web (prioritaire sur la variable WEB_PORT)")
	flag.CommandLine.Parse(args)

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

	router := web.SetupServer(cfg.ExchangeName, db, cli.RootWd, cfg.GetLogFile())
	router.Run(effectivePort)
}
