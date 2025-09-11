package main

import (
	"bot/internal/loader"
	"bot/internal/logger"
	"flag"
	"log"
	"os"
)

func main() {
	projectRoot, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)
	log.Printf("=== Bot Volatility ===")

	// Paramètres de ligne de commande
	var (
		botDir = flag.String("root", ".", "Path to the bot directory")
	)
	flag.Parse()

	bot, err := loader.LoadBot(projectRoot, *botDir)
	if err != nil {
		log.Fatalf("Failed to load bot: %v", err)
	}
	defer bot.Cleanup()

	volatility, err := bot.Calculator.CalculateVolatility(bot.Config.Pair, "4h", 7*6)
	if err != nil {
		logger.Fatalf("Failed to compute Volatility: %v", err)
	}

	logger.Infof("Volatility: %.2f%%", volatility)
}
