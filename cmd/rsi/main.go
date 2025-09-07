package main

import (
	"bot/internal/core/config"
	"bot/internal/exchange"
	"bot/internal/logger"
	"flag"
	"log"
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
	log.Printf("=== Bot RSI ===")

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

	// 1 mois
	since := time.Now().AddDate(0, 0, -1).UnixMilli()
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

	if len(candles) < 2 {
		logger.Fatalf("not enough candle data for RSI calculation")
	}

	// Calculer les gains et pertes
	gains := make([]float64, len(candles)-1)
	losses := make([]float64, len(candles)-1)

	for i := 1; i < len(candles); i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains[i-1] = change
			losses[i-1] = 0
		} else {
			gains[i-1] = 0
			losses[i-1] = -change
		}
	}

	// Calculer les moyennes mobiles exponentielles des gains et pertes
	avgGain := gains[0]
	avgLoss := losses[0]

	for i := 1; i < len(gains); i++ {
		avgGain = (avgGain*(float64(24)-1) + gains[i]) / float64(24)
		avgLoss = (avgLoss*(float64(24)-1) + losses[i]) / float64(24)
	}

	// Calculer le RSI
	var rsi float64
	if avgLoss == 0 {
		rsi = 100.0
	}

	rs := avgGain / avgLoss
	rsi = 100.0 - (100.0 / (1.0 + rs))

	logger.Infof("RSI: %.2f", rsi)
}
