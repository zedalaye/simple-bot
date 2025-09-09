package main

import (
	"bot/internal/core/config"
	"bot/internal/exchange"
	"bot/internal/logger"
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/joho/godotenv"
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

	orderId := flag.Arg(0)
	if orderId == "" {
		log.Fatalf("Usage: bin/order -root <bot_directory> <order_id>")
	}

	// Changer le répertoire de travail si nécessaire
	if *botDir != "." {
		err := os.Chdir(*botDir)
		if err != nil {
			log.Fatalf("Failed to change directory to %s: %v", *botDir, err)
		}
	}

	fileConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	err = logger.InitLogger(fileConfig.GetLogLevel(), fileConfig.GetLogFile())
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	err = godotenv.Load(fileConfig.EnvFilePaths()...)
	if err != nil {
		logger.Warn("⚠️ No .env file found, using system environment variables")
	}

	// Retourne au dossier racine par défaut
	err = os.Chdir(projectRoot)
	if err != nil {
		logger.Fatalf("Failed to change directory back to %s: %v", projectRoot, err)
	}

	// 2. Créer l'instance de l'exchange
	exchg := exchange.NewExchange(fileConfig.Exchange.Name)
	if exchg == nil {
		logger.Fatalf("Failed to create %s exchange instance", fileConfig.Exchange.Name)
	}

	order, err := exchg.FetchOrder(orderId, fileConfig.Trading.Pair)
	if err != nil {
		logger.Fatalf("Failed to fetch order %s: %v", orderId, err)
	}
	orderJson, err := json.MarshalIndent(order, "", "  ")
	if err != nil {
		logger.Fatalf("Failed to marshal order to JSON: %v", err)
	}
	logger.Infof("Order %s details:\n%s", orderId, string(orderJson))

	trades, err := exchg.FetchTradesForOrder(orderId, fileConfig.Trading.Pair)
	if err != nil {
		logger.Fatalf("Failed to fetch trades for order: %v", err)
	}
	for _, trade := range trades {
		tradeJson, err := json.MarshalIndent(trade, "", "  ")
		if err != nil {
			logger.Errorf("Failed to marshal trade to JSON: %v", err)
			continue
		}
		logger.Infof("Trade for order %s:\n%s", orderId, string(tradeJson))
	}
}
