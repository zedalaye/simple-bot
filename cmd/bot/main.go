package main

import (
	"bot/internal/bot"
	"bot/internal/config"
	"bot/internal/database"
	"bot/internal/logger"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ccxt/ccxt/go/v4"
)

func main() {
	log.Println("Starting Simple Bot")

	// Paramètres de ligne de commande
	configFile := flag.String("config", "config.yml", "Path to configuration file (YAML format)")
	flag.Parse()

	// Chargement de la configuration
	fileConfig, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialisation du système de logs
	err = logger.InitLogger(fileConfig.GetLogLevel(), fileConfig.GetLogFile())
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Conversion vers la configuration du bot
	botConfig := fileConfig.ToBotConfig()

	logger.Infof("Configuration loaded: Pair=%s, Amount=%v, PriceOffset=%v, Threshold=%v",
		botConfig.Pair, botConfig.QuoteAmount, botConfig.PriceOffset, botConfig.ProfitThreshold)

	// Initialisation de la base de données
	db, err := database.NewDB(fileConfig.Database.Path)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	logger.Info("Database initialized successfully")

	// Configuration de l'exchange
	exchange := createExchange(fileConfig.Exchange.Name)
	if exchange == nil {
		logger.Fatalf("Failed to create %s exchange instance", fileConfig.Exchange.Name)
	}
	logger.Infof("%s exchange initialized", fileConfig.Exchange.Name)

	// Création et démarrage du bot
	tradingBot, err := bot.NewBot(botConfig, db, exchange)
	if err != nil {
		logger.Fatalf("Failed to create bot: %v", err)
	}

	err = tradingBot.Start()
	if err != nil {
		logger.Fatalf("Failed to start bot: %v", err)
	}

	// Gestion des signaux d'arrêt
	waitForShutdown(tradingBot)
}

func createExchange(exchangeName string) ccxt.IExchange {
	return ccxt.CreateExchange(exchangeName, map[string]interface{}{
		"apiKey":          os.Getenv("API_KEY"),
		"secret":          os.Getenv("API_SECRET"),
		"enableRateLimit": true,
	})
}

func waitForShutdown(tradingBot *bot.Bot) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	<-sigs
	logger.Info("Got a stop signal. Stopping bot...")

	tradingBot.Stop()
	time.Sleep(1 * time.Second)

	tradingBot.ShowFinalStats()
	logger.Info("Simple Bot Stopped. See Ya!")
}
