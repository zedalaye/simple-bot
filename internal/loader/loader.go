package loader

import (
	"bot/internal/bot"
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/exchange"
	"bot/internal/logger"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func LoadBot(projectRoot, botDir string) (*bot.Bot, error) {
	if botDir != "." {
		err := os.Chdir(botDir)
		if err != nil {
			log.Fatalf("Failed to change directory to %s: %v", botDir, err)
		}
	}
	// En sortant, retourne au dossier racine par défaut
	defer func(dir string) {
		err := os.Chdir(dir)
		if err != nil {
			log.Fatalf("Failed to change directory back to %s: %v", projectRoot, err)
		}
	}(projectRoot)

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
	logger.Info("Database initialized successfully")

	// Configuration de l'exchange
	exchg := exchange.NewExchange(fileConfig.Exchange.Name)
	logger.Infof("%s exchange initialized", fileConfig.Exchange.Name)

	// Création du bot
	tradingBot, err := bot.NewBot(botConfig, db, exchg)
	if err != nil {
		logger.Fatalf("Failed to create bot: %v", err)
	}

	return tradingBot, nil
}
