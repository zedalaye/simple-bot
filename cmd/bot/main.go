package main

import (
	"bot/internal/bot"
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/exchange"
	"bot/internal/logger"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	projectRoot, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)
	log.Println("Starting Simple Bot")

	// Paramètres de ligne de commande
	var (
		botDir      = flag.String("root", ".", "Path to the bot directory")
		buyAtLaunch = flag.Bool("buy-at-launch", false, "Immediately place a buy order after startup")
	)
	flag.Parse()

	// Changer le répertoire de travail si nécessaire
	if *botDir != "." {
		err := os.Chdir(*botDir)
		if err != nil {
			log.Fatalf("Failed to change directory to %s: %v", *botDir, err)
		}
	}

	// Chargement de la configuration
	fileConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialisation du système de logs
	err = logger.InitLogger(fileConfig.GetLogLevel(), fileConfig.GetLogFile())
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Charge le fichier .env pour obtenir les API Keys
	err = godotenv.Load(fileConfig.EnvFilePaths()...)
	if err != nil {
		logger.Warn("No .env file found, using system environment variables")
	}

	// Conversion vers la configuration du bot
	botConfig := fileConfig.ToBotConfig()

	logger.Infof("Configuration loaded: Pair=%s, Amount=%v, MaxBuys/Day=%d, RSIThreshold=%.2f, ProfitTarget=%.2f, VolatilityAdjustmment=%.2f",
		botConfig.Pair, botConfig.QuoteAmount, botConfig.MaxBuysPerDay, botConfig.RSIThreshold, botConfig.ProfitTarget, botConfig.VolatilityAdjustment)

	// Initialisation de la base de données
	db, err := database.NewDB(fileConfig.Database.Path)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer func(db *database.DB) {
		err := db.Close()
		if err != nil {
			logger.Fatalf("Failed to close database: %v", err)
		}
	}(db)
	logger.Info("Database initialized successfully")

	// Retourne au dossier racine par défaut
	err = os.Chdir(projectRoot)
	if err != nil {
		log.Fatalf("Failed to change directory back to %s: %v", projectRoot, err)
	}

	// Configuration de l'exchange
	exchg := exchange.NewExchange(fileConfig.Exchange.Name)
	logger.Infof("%s exchange initialized", fileConfig.Exchange.Name)

	// Création et démarrage du bot
	tradingBot, err := bot.NewBot(botConfig, db, exchg)
	if err != nil {
		logger.Fatalf("Failed to create bot: %v", err)
	}

	err = tradingBot.Start(*buyAtLaunch)
	if err != nil {
		logger.Fatalf("Failed to start bot: %v", err)
	}

	// Gestion des signaux d'arrêt
	waitForShutdown(tradingBot)
}

func waitForShutdown(tradingBot *bot.Bot) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	<-sigs
	logger.Info("Got a stop signal. Stopping bot...")

	tradingBot.Stop()
	time.Sleep(1 * time.Second)

	tradingBot.ShowStatistics()
	logger.Info("Simple Bot Stopped. See Ya!")
}
