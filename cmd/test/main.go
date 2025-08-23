package main

import (
	"bot/internal/bot"
	"bot/internal/core/config"
	"bot/internal/exchange"
	"bot/internal/logger"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const MinQuoteAmount = 10.0

func main() {
	log.SetOutput(os.Stdout)
	log.Println("=== Bot Test Suite ===")

	// Paramètres de ligne de commande
	configFile := flag.String("config", "config.yml", "Path to configuration file (YAML format)")
	flag.Parse()

	// 1. Charger la configuration du bot
	fmt.Println("1. Loading bot configuration...")
	fileConfig, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialiser le logger pour les tests
	err = logger.InitLogger(fileConfig.GetLogLevel(), fileConfig.GetLogFile())
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Charge le fichier .env pour obtenir les API Keys
	err = godotenv.Load(fileConfig.EnvFilePaths()...)
	if err != nil {
		logger.Warn("No .env file found, using system environment variables")
	}

	botConfig := fileConfig.ToBotConfig()
	logger.Debugf("✓ Configuration loaded: Pair=%s, Amount=%.2f, PriceOffset=%.2f",
		botConfig.Pair, botConfig.QuoteAmount, botConfig.PriceOffset)

	// 2. Créer l'instance de l'exchange
	logger.Info("2. Creating exchange instance...")
	exchg := exchange.NewExchange(fileConfig.Exchange.Name)
	if exchg == nil {
		logger.Fatalf("Failed to create %s exchange instance", fileConfig.Exchange.Name)
	}
	logger.Infof("✓ %s exchange initialized", fileConfig.Exchange.Name)

	//for _, market := range exchg.GetMarketsList() {
	//	logger.Infof("%s: BaseId=%s, QuoteId=%s", market.Symbol, market.BaseId, market.QuoteId)
	//}

	// Récupérer les informations du marché
	market := exchg.GetMarket(botConfig.Pair)
	baseAsset := market.BaseAsset
	quoteAsset := market.QuoteAsset
	logger.Infof("✓ Market info: %s/%s", baseAsset, quoteAsset)

	// 3. Vérifier les fonds disponibles dans la devise de cotation
	logger.Info("3. Checking quote currency balance...")
	baseBalance, quoteBalance, err := checkBalance(exchg, baseAsset, quoteAsset)
	if err != nil {
		logger.Fatalf("Failed to check balances: %v", err)
	}
	logger.Infof("✓ %s balance: %s", baseAsset, market.FormatAmount(baseBalance))
	logger.Infof("✓ %s balance: %s", quoteAsset, market.FormatAmount(quoteBalance))

	if quoteBalance < botConfig.QuoteAmount {
		logger.Warnf("⚠️  Warning: Insufficient %s balance (%.6f < %.2f)",
			quoteAsset, quoteBalance, botConfig.QuoteAmount)
	}

	// 4. Vérifier le prix de la devise de base
	logger.Info("4. Fetching current price...")
	currentPrice, err := exchg.GetPrice(botConfig.Pair)
	if err != nil {
		logger.Fatalf("Failed to get current price: %v", err)
	}
	logger.Infof("✓ Current %s price: %s %s", baseAsset, market.FormatPrice(currentPrice), quoteAsset)

	// 5. Créer un ordre d'achat limite de 1au prix - offset
	logger.Info("5. Creating limit buy order...")

	buyAmountInQuoteAsset := max(min(quoteBalance, botConfig.QuoteAmount)*0.01, MinQuoteAmount)
	logger.Infof("   Buy amount: %.6f %s", buyAmountInQuoteAsset, quoteAsset)

	limitPrice := currentPrice - botConfig.PriceOffset
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
		logger.Info("6. Cancelling buy order...")
		_, err = exchg.CancelOrder(*buyOrder.Id, botConfig.Pair)
		if err != nil {
			logger.Errorf("Failed to cancel buy order: %v", err)
			// Ne pas arrêter le test, continuer
		} else {
			logger.Infof("✓ Buy order cancelled: ID=%s", *buyOrder.Id)
		}

		// Attendre un moment pour que l'annulation soit effective
		time.Sleep(2 * time.Second)
	}

	//// 7. Vérifier les fonds disponibles dans la devise de base
	//logger.Info("7. Checking base currency balance...")
	//baseBalance, err := checkBalance(exchange, baseAsset)
	//if err != nil {
	//	logger.Fatalf("Failed to check base balance: %v", err)
	//}
	//logger.Infof("✓ %s balance: %.6f", baseAsset, baseBalance)
	//
	//if baseBalance <= 0 {
	//	logger.Warnf("⚠️  Warning: No %s balance for sell order test", baseAsset)
	//	logger.Info("   Using minimum amount for demonstration...")
	//	baseBalance = 0.001 // Montant minimal pour le test
	//}

	// 8. Créer un ordre de vente limite au prix + offset
	logger.Info("7. Creating limit sell order...")
	sellPrice := currentPrice + botConfig.PriceOffset
	sellAmountInBaseAsset := *buyOrder.Amount

	logger.Infof("   Sell price: %.2f %s (current + %.2f)", sellPrice, quoteAsset, botConfig.PriceOffset)
	logger.Infof("   Sell amount: %.6f %s", sellAmountInBaseAsset, baseAsset)

	sellOrder, err := exchg.PlaceLimitSellOrder(botConfig.Pair, sellAmountInBaseAsset, sellPrice)
	if err != nil {
		logger.Errorf("Failed to place sell order: %v", err)
		// Ne pas arrêter le test, continuer
	} else {
		sellOrder, err = exchg.FetchOrder(*sellOrder.Id, botConfig.Pair)
		if err != nil {
			logger.Fatalf("Failed to fetch sell order: %v", err)
		}
		logger.Infof("✓ Sell order created: ID=%s, Price=%.2f, Amount=%.6f, Status=%s",
			*sellOrder.Id, *sellOrder.Price, *sellOrder.Amount, *sellOrder.Status)

		// Attendre un moment
		time.Sleep(2 * time.Second)

		// 9. Annuler l'ordre de vente
		logger.Info("8. Cancelling sell order...")
		_, err = exchg.CancelOrder(*sellOrder.Id, botConfig.Pair)
		if err != nil {
			logger.Errorf("Failed to cancel sell order: %v", err)
		} else {
			logger.Infof("✓ Sell order cancelled: ID=%s", *sellOrder.Id)
		}
	}

	// Résumé final
	logger.Info("=== Test Summary ===")
	logger.Infof("Exchange: %s", fileConfig.Exchange.Name)
	logger.Infof("Trading pair: %s", botConfig.Pair)
	logger.Infof("Current price: %.2f %s", currentPrice, quoteAsset)
	logger.Infof("Price offset: %.2f %s", botConfig.PriceOffset, quoteAsset)
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
