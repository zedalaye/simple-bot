// Package ordercli implémente la sous-commande « order » : inspecte un ordre et ses trades.
package ordercli

import (
	"bot/internal/exchange"
	"bot/internal/loader"
	"bot/internal/logger"
	"encoding/json"
	"flag"
	"log"
)

// Main est le point d'entrée de la sous-commande « order ». Le flag --root et le chdir
// sont gérés en amont par le dispatcher (cmd/simple-bot).
func Main(args []string) {
	flag.CommandLine.Parse(args)

	orderId := flag.Arg(0)
	if orderId == "" {
		log.Fatalf("Usage: simple-bot [--root <dir>] order <order_id>")
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

	exchg := exchange.NewExchange(cfg.ExchangeName)
	if exchg == nil {
		logger.Fatalf("Failed to create %s exchange instance", cfg.ExchangeName)
	}

	order, err := exchg.FetchOrder(orderId, cfg.TradingPair)
	if err != nil {
		logger.Fatalf("Failed to fetch order %s: %v", orderId, err)
	}
	orderJson, err := json.MarshalIndent(order, "", "  ")
	if err != nil {
		logger.Fatalf("Failed to marshal order to JSON: %v", err)
	}
	logger.Infof("Order %s details:\n%s", orderId, string(orderJson))

	trades, err := exchg.FetchTradesForOrder(orderId, cfg.TradingPair)
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
