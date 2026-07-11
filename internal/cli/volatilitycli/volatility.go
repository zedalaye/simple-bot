// Package volatilitycli implémente la sous-commande « volatility ».
package volatilitycli

import (
	"bot/internal/loader"
	"bot/internal/logger"
	"flag"
	"log"
)

// Main est le point d'entrée de la sous-commande « volatility ». Le flag --root et le
// chdir sont gérés en amont par le dispatcher (cmd/simple-bot).
func Main(args []string) {
	log.Printf("=== Bot Volatility ===")

	flag.CommandLine.Parse(args)

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
