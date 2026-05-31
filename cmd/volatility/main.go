package main

import (
	"bot/internal/loader"
	"bot/internal/logger"
	"flag"
	"log"
	"os"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Printf("=== Bot Volatility ===")

	botDir := flag.String("root", ".", "Répertoire racine de l'instance du bot")
	flag.Parse()

	if *botDir != "." {
		if err := os.Chdir(*botDir); err != nil {
			log.Fatalf("Impossible de changer de répertoire vers %s : %v", *botDir, err)
		}
	}

	bot, err := loader.LoadBot()
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
