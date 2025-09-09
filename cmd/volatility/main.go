package main

import (
	"bot/internal/core/config"
	"bot/internal/exchange"
	"bot/internal/logger"
	"flag"
	"log"
	"math"
	"os"
	"time"

	"github.com/joho/godotenv"
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

	// Changer le répertoire de travail si nécessaire
	if *botDir != "." {
		err := os.Chdir(*botDir)
		if err != nil {
			log.Fatalf("Failed to change directory to %s: %v", *botDir, err)
		}
	}

	// 1. Charger la configuration du bot
	log.Println("1. Loading bot configuration...")
	fileConfig, err := config.LoadConfig()
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

	// Retourne au dossier racine par défaut
	err = os.Chdir(projectRoot)
	if err != nil {
		log.Fatalf("Failed to change directory back to %s: %v", projectRoot, err)
	}

	// 2. Créer l'instance de l'exchange
	logger.Info("2. Creating exchange instance...")
	exchg := exchange.NewExchange(fileConfig.Exchange.Name)
	if exchg == nil {
		logger.Fatalf("Failed to create %s exchange instance", fileConfig.Exchange.Name)
	}
	logger.Infof("✓ %s exchange initialized", fileConfig.Exchange.Name)

	since := time.Now().AddDate(0, 0, -botConfig.VolatilityPeriod).UnixMilli()
	candles, err := exchg.FetchCandles(botConfig.Pair, "1h", &since, 500)
	if err != nil {
		logger.Fatalf("Failed to fetch candles: %v", err)
	}

	logger.Infof("Got %d candles", len(candles))

	// Extrait les prix de clôture
	prices := make([]float64, len(candles))
	for i, candle := range candles {
		prices[i] = candle.Close
	}

	if len(prices) < 2 {
		logger.Fatalf("not enough price data for volatility calculation")
	}

	// Calculer les rendements quotidiens
	returns := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		returns[i-1] = (prices[i] - prices[i-1]) / prices[i-1]
	}

	// Calculer la moyenne des rendements
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Calculer la variance
	var variance float64
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	variance /= float64(len(returns))

	// Volatilité = écart-type (racine carrée de la variance)
	volatility := math.Sqrt(variance)

	logger.Infof("Volatility: %.2f", volatility*100)
}
