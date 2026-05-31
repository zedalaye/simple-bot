package loader

import (
	"bot/internal/bot"
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/exchange"
	"bot/internal/logger"
	"fmt"
	"log"

	"github.com/joho/godotenv"
)

// LoadConfig charge le fichier .env, lit la configuration depuis l'environnement,
// initialise le logger et ouvre la base de données.
func LoadConfig() (config.AppConfig, *database.DB, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	cfg := config.Load()

	if err := logger.InitLogger(cfg.GetLogLevel(), cfg.GetLogFile()); err != nil {
		return config.AppConfig{}, nil, fmt.Errorf("logger : %w", err)
	}

	db, err := database.NewDB(cfg.DBPath)
	if err != nil {
		return config.AppConfig{}, nil, fmt.Errorf("base de données : %w", err)
	}

	logger.Info("✓ Configuration chargée")
	logger.Infof("  Exchange        %s", cfg.ExchangeName)
	logger.Infof("  Paire           %s", cfg.TradingPair)
	logger.Infof("  Check interval  %v", cfg.CheckInterval)
	logger.Infof("  Port web        %s", cfg.WebPort)

	return cfg, db, nil
}

// LoadBot charge la configuration et initialise le bot complet.
func LoadBot() (*bot.Bot, error) {
	cfg, db, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	exchg := exchange.NewExchange(cfg.ExchangeName)
	logger.Infof("[%s] ✓ Exchange initialisé", cfg.ExchangeName)

	tradingBot, err := bot.NewBot(cfg.ToBotConfig(), db, exchg)
	if err != nil {
		return nil, fmt.Errorf("création du bot : %w", err)
	}
	logger.Infof("[%s] ✓ Bot initialisé", cfg.ExchangeName)

	return tradingBot, nil
}
