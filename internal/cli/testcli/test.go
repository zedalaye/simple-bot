// Package testcli implémente la sous-commande « test » : suite de tests exchange (ordres réels).
package testcli

import (
	"bot/internal/bot"
	"bot/internal/exchange"
	"bot/internal/loader"
	"bot/internal/logger"
	"flag"
	"log"
	"time"
)

const MinQuoteAmount = 10.0

// testPriceOffset : écart au cours des ordres de test (1 %). Assez large pour qu'un
// ordre post-only ne s'exécute pas pendant les quelques secondes où il est ouvert.
const testPriceOffset = 0.01

var step = 0

func logStep(fmt string, v ...any) {
	step = step + 1
	if logger.IsInitialized() {
		logger.Infof("%d. "+fmt, append([]any{step}, v...)...)
	} else {
		log.Printf("%d. "+fmt, append([]any{step}, v...)...)
	}
}

// Main est le point d'entrée de la sous-commande « test ». Le flag --root et le chdir
// sont gérés en amont par le dispatcher (cmd/simple-bot).
func Main(args []string) {
	flag.CommandLine.Parse(args)

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

	// 5-8. Poser puis annuler un achat et une vente, assez loin du cours (±1 %)
	// pour que ces ordres post-only ne puissent pas s'exécuter pendant le test.
	buyAmountInQuoteAsset := max(min(quoteBalance, 50.0)*0.01, MinQuoteAmount)
	buyPrice := currentPrice * (1 - testPriceOffset)
	baseAmount := buyAmountInQuoteAsset / buyPrice
	sellPrice := currentPrice * (1 + testPriceOffset)

	ok := placeAndCancel(exchg, market, botConfig.Pair, "buy", baseAmount, buyPrice)
	time.Sleep(2 * time.Second)
	ok = placeAndCancel(exchg, market, botConfig.Pair, "sell", baseAmount, sellPrice) && ok

	// Résumé final
	logger.Info("=== Test Summary ===")
	logger.Infof("Exchange: %s", cfg.ExchangeName)
	logger.Infof("Trading pair: %s", botConfig.Pair)
	logger.Infof("Current price: %.2f %s", currentPrice, quoteAsset)
	logger.Infof("%s balance: %.6f", quoteAsset, quoteBalance)
	logger.Infof("%s balance: %.6f", baseAsset, baseBalance)
	if !ok {
		logger.Fatalf("✗ Test en échec : vérifier sur l'exchange qu'aucun ordre de test n'est resté ouvert")
	}
	logger.Info("✓ All tests completed successfully!")
}

// placeAndCancel pose un ordre limite (post-only), l'annule, puis vérifie qu'il est bien
// annulé. Renvoie false si une étape échoue ; dans ce cas l'id est loggé pour pouvoir
// contrôler (et annuler à la main) l'ordre sur l'exchange.
func placeAndCancel(exchg *exchange.Exchange, market bot.Market, pair, side string, amount, price float64) bool {
	logStep("Create limit %s order (%s %s @ %s)...", side,
		market.FormatAmount(amount), market.BaseAsset, market.FormatPrice(price))

	var order bot.Order
	var err error
	if side == "buy" {
		order, err = exchg.PlaceLimitBuyOrder(pair, amount, price)
	} else {
		order, err = exchg.PlaceLimitSellOrder(pair, amount, price)
	}
	if err != nil {
		logger.Errorf("✗ Failed to place %s order: %v", side, err)
		return false
	}
	id := *order.Id
	logger.Infof("✓ %s order created: ID=%s, Price=%s, Amount=%s, Status=%s", side, id,
		market.FormatPrice(*order.Price), market.FormatAmount(*order.Amount), *order.Status)

	time.Sleep(2 * time.Second)

	logStep("Cancel %s order %s...", side, id)
	if _, err := exchg.CancelOrder(id, pair); err != nil {
		// L'annulation peut avoir réussi même si la relecture qui suit a échoué :
		// on tranche avec la vérification ci-dessous.
		logger.Warnf("Cancel %s order %s a renvoyé : %v", side, id, err)
	}

	// Vérifier l'état final (quelques relectures : l'exchange peut mettre un instant)
	for attempt := 0; attempt < 5; attempt++ {
		time.Sleep(time.Second)
		final, err := exchg.FetchOrder(id, pair)
		if err != nil {
			logger.Warnf("Relecture de l'ordre %s : %v", id, err)
			continue
		}
		if final.Status != nil && *final.Status == "canceled" {
			logger.Infof("✓ %s order cancelled: ID=%s", side, id)
			return true
		}
		logger.Errorf("✗ %s order %s : statut %s au lieu de canceled", side, id, *final.Status)
		return false
	}
	logger.Errorf("✗ %s order %s : état final introuvable, à vérifier sur l'exchange", side, id)
	return false
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
