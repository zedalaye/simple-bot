// Package rsicli implémente la sous-commande « rsi » : calcule le RSI courant.
package rsicli

import (
	"bot/internal/loader"
	"bot/internal/logger"
	"flag"
	"log"
)

// Main est le point d'entrée de la sous-commande « rsi ». Le flag --root et le chdir
// sont gérés en amont par le dispatcher (cmd/simple-bot).
func Main(args []string) {
	log.Printf("=== Bot RSI ===")

	flag.CommandLine.Parse(args)

	bot, err := loader.LoadBot()
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
