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

// LoadOffline charge l'environnement de l'instance (.env) et ouvre la base en mode
// silencieux (logger à "error"), pour les outils d'analyse hors-ligne (backtest,
// patternscan) qui ne touchent pas l'exchange et ne doivent pas polluer stdout.
func LoadOffline() (config.AppConfig, *database.DB, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	cfg := config.Load()

	// NewDB logge pendant les migrations : initialiser le logger sinon nil deref.
	if err := logger.InitLogger("error", ""); err != nil {
		return config.AppConfig{}, nil, fmt.Errorf("logger : %w", err)
	}

	db, err := database.NewDB(cfg.DBPath)
	if err != nil {
		return config.AppConfig{}, nil, fmt.Errorf("base de données : %w", err)
	}

	return cfg, db, nil
}

// LoadBot charge la configuration et initialise le bot complet.
func LoadBot() (*bot.Bot, error) {
	tradingBot, _, err := LoadBotWithConfig()
	return tradingBot, err
}

// LoadBotWithConfig fait le même travail que LoadBot mais rend aussi la
// configuration applicative, dont le daemon a besoin pour monter les services
// périphériques (relay de notifications) que le bot lui-même ignore.
func LoadBotWithConfig() (*bot.Bot, config.AppConfig, error) {
	cfg, db, err := LoadConfig()
	if err != nil {
		return nil, config.AppConfig{}, err
	}

	exchg := exchange.NewExchange(cfg.ExchangeName)
	logger.Infof("[%s] ✓ Exchange initialisé", cfg.ExchangeName)

	tradingBot, err := bot.NewBot(cfg.ToBotConfig(), db, exchg)
	if err != nil {
		return nil, config.AppConfig{}, fmt.Errorf("création du bot : %w", err)
	}
	logger.Infof("[%s] ✓ Bot initialisé", cfg.ExchangeName)

	return tradingBot, cfg, nil
}
