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
	log.Printf("=== Bot RSI ===")

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

	rsi, err := bot.Calculator.CalculateRSI(bot.Config.Pair, "4h", 14)
	if err != nil {
		logger.Fatalf("Failed to compute RSI: %v", err)
	}

	logger.Infof("RSI : %.2f", rsi)
}
