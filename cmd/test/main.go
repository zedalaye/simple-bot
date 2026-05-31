package main

import (
	"bot/internal/bot"
	"bot/internal/exchange"
	"bot/internal/loader"
	"bot/internal/logger"
	"flag"
	"log"
	"os"
	"time"
)

const MinQuoteAmount = 10.0

var step = 0

func logStep(fmt string, v ...any) {
	step = step + 1
	if logger.IsInitialized() {
		logger.Infof("%d. "+fmt, append([]any{step}, v...)...)
	} else {
		log.Printf("%d. "+fmt, append([]any{step}, v...)...)
	}
}

func main() {
	log.SetOutput(os.Stdout)
	botDir := flag.String("root", ".", "Répertoire racine de l'instance du bot")
	flag.Parse()

	if *botDir != "." {
		if err := os.Chdir(*botDir); err != nil {
			log.Fatalf("Impossible de changer de répertoire vers %s : %v", *botDir, err)
		}
	}

	log.Println("=== Bot Test Suite ===")

	// 1. Charger la configuration
	logStep("Loading configuration...")
	cfg, db, err := loader.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Fatalf("Failed to close database: %v", err)
		}
	}()

	botConfig := cfg.ToBotConfig()
	logger.Infof("✓ Configuration loaded: Exchange=%s, Pair=%s, CheckInterval=%v",
		botConfig.ExchangeName, botConfig.Pair, botConfig.CheckInterval)

	// 2. Créer l'instance de l'exchange
	logStep("Creating exchange instance...")
	exchg := exchange.NewExchange(cfg.ExchangeName)
	if exchg == nil {
		logger.Fatalf("Failed to create %s exchange instance", cfg.ExchangeName)
	}
	logger.Infof("✓ %s exchange initialized", cfg.ExchangeName)

	// logStep("Load all markets list...")
	//for _, market := range exchg.GetMarketsList() {
	//	logger.Infof("%s: BaseId=%s, QuoteId=%s", market.Symbol, market.BaseId, market.QuoteId)
	//}

	// Récupérer les informations du marché
	logStep("Get Market Info...")
	market := exchg.GetMarket(botConfig.Pair)
	baseAsset := market.BaseAsset
	quoteAsset := market.QuoteAsset
	logger.Infof("✓ Market info: %s/%s", baseAsset, quoteAsset)

	// 3. Vérifier les fonds disponibles dans la devise de cotation
	logStep("Checking quote currency balance...")
	baseBalance, quoteBalance, err := checkBalance(exchg, baseAsset, quoteAsset)
	if err != nil {
		logger.Fatalf("Failed to check balances: %v", err)
	}
	logger.Infof("✓ %s balance: %s", baseAsset, market.FormatAmount(baseBalance))
	logger.Infof("✓ %s balance: %s", quoteAsset, market.FormatAmount(quoteBalance))

	// Note: Trading amounts are now configured per strategy, not globally
	if quoteBalance < MinQuoteAmount {
		logger.Warnf("⚠️  Warning: Insufficient %s balance (%.6f < %.2f)",
			quoteAsset, quoteBalance, MinQuoteAmount)
	}

	// 4. Vérifier le prix de la devise de base
	logStep("Fetching current price...")
	currentPrice, err := exchg.GetPrice(botConfig.Pair)
	if err != nil {
		logger.Fatalf("Failed to get current price: %v", err)
	}
	logger.Infof("✓ Current %s price: %s %s", baseAsset, market.FormatPrice(currentPrice), quoteAsset)

	//logStep("Load all known orders...")
	//orders, err := db.GetAllOrders()
	//if err != nil {
	//	logger.Errorf("Failed to get all orders: %v", err)
	//} else {
	//	for _, dbOrder := range orders {
	//		exchgOrder, err := exchg.FetchOrder(dbOrder.ExternalID, botConfig.Pair)
	//		if err != nil {
	//			logger.Errorf("Failed to fetch order from exchange: %v", err)
	//		} else {
	//			logger.Infof("Order %s, FeeRate=%f, Fee=%f", *exchgOrder.Id, *exchgOrder.FeeRate, *exchgOrder.Fee)
	//		}
	//	}
	//}

	//logStep("Load all known trades...")
	//trades, err := exchg.FetchTrades(botConfig.Pair, nil, 500)
	//if err != nil {
	//	logger.Fatalf("Failed to fetch trades: %v", err)
	//} else {
	//	for _, trade := range trades {
	//		logger.Infof("✓ Trade %s (%s), FeeToken=%s, Fee=%.9f", *trade.Id, *trade.OrderId, *trade.FeeToken, *trade.Fee)
	//	}
	//}

	// 5. Créer un ordre d'achat limite de 1au prix - offset
	logStep("Create limit buy order...")

	// Use a small test amount since quote amounts are now configured per strategy
	buyAmountInQuoteAsset := max(min(quoteBalance, 50.0)*0.01, MinQuoteAmount)
	logger.Infof("   Test buy amount: %.6f %s", buyAmountInQuoteAsset, quoteAsset)

	// For testing, use a simple fixed offset (similar to old behavior)
	testOffset := 10.0 // Use a small fixed offset for testing
	limitPrice := currentPrice - testOffset
	baseAmount := buyAmountInQuoteAsset / limitPrice

	buyOrder, err := exchg.PlaceLimitBuyOrder(botConfig.Pair, baseAmount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place buy order: %v", err)
	} else {
		buyOrder, err = exchg.FetchOrder(*buyOrder.Id, botConfig.Pair)
		if err != nil {
			logger.Fatalf("Failed to fetch buy order: %v", err)
		}
		logger.Infof("✓ Buy order created: ID=%s, Price=%.2f, Amount=%.6f, Status=%s",
			*buyOrder.Id, *buyOrder.Price, *buyOrder.Amount, *buyOrder.Status)

		// Attendre un moment pour que l'ordre soit enregistré
		time.Sleep(2 * time.Second)

		// 6. Annuler l'ordre d'achat
		logStep("Cancel buy order...")
		_, err = exchg.CancelOrder(*buyOrder.Id, botConfig.Pair)
		if err != nil {
			logger.Errorf("Failed to cancel buy order: %v", err)
		} else {
			logger.Infof("✓ Buy order cancelled: ID=%s", *buyOrder.Id)
		}

		// Attendre un moment pour que l'annulation soit effective
		time.Sleep(2 * time.Second)

		// 7. Créer un ordre de vente limite au prix + offset
		logStep("Create limit sell order...")
		sellOffset := currentPrice * 0.002
		sellPrice := currentPrice + sellOffset
		sellAmountInBaseAsset := *buyOrder.Amount

		logger.Infof("   Sell price: %.2f %s (current + %.2f)", sellPrice, quoteAsset, sellOffset)
		logger.Infof("   Sell amount: %.6f %s", sellAmountInBaseAsset, baseAsset)

		sellOrder, err := exchg.PlaceLimitSellOrder(botConfig.Pair, sellAmountInBaseAsset, sellPrice)
		if err != nil {
			logger.Errorf("Failed to place sell order: %v", err)
		} else {
			sellOrder, err = exchg.FetchOrder(*sellOrder.Id, botConfig.Pair)
			if err != nil {
				logger.Fatalf("Failed to fetch sell order: %v", err)
			}
			logger.Infof("✓ Sell order created: ID=%s, Price=%.2f, Amount=%.6f, Status=%s",
				*sellOrder.Id, *sellOrder.Price, *sellOrder.Amount, *sellOrder.Status)

			// Attendre un moment
			time.Sleep(2 * time.Second)

			// 8. Annuler l'ordre de vente
			logStep("Cancel sell order...")
			_, err = exchg.CancelOrder(*sellOrder.Id, botConfig.Pair)
			if err != nil {
				logger.Errorf("Failed to cancel sell order: %v", err)
			} else {
				logger.Infof("✓ Sell order cancelled: ID=%s", *sellOrder.Id)
			}
		}
	}

	// Résumé final
	logger.Info("=== Test Summary ===")
	logger.Infof("Exchange: %s", cfg.ExchangeName)
	logger.Infof("Trading pair: %s", botConfig.Pair)
	logger.Infof("Current price: %.2f %s", currentPrice, quoteAsset)
	logger.Infof("%s balance: %.6f", quoteAsset, quoteBalance)
	logger.Infof("%s balance: %.6f", baseAsset, baseBalance)
	logger.Info("✓ All tests completed successfully!")
}

func checkBalance(exchange bot.Exchange, baseAsset, quoteAsset string) (float64, float64, error) {
	balances, err := exchange.FetchBalance()
	if err != nil {
		return 0, 0, err
	}

	baseBalance, ok1 := balances[baseAsset]
	if !ok1 {
		baseBalance.Free = 0
	}
	quoteBalance, ok2 := balances[quoteAsset]
	if !ok2 {
		quoteBalance.Free = 0
	}

	return baseBalance.Free, quoteBalance.Free, nil
}
