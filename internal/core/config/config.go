package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// AppConfig contient tous les paramètres de l'application, lus depuis les variables d'environnement.
type AppConfig struct {
	ExchangeName   string
	TradingPair    string
	CheckInterval  time.Duration
	DBPath         string
	LogLevel       string
	LogFile        string
	WebPort        string
	HealthcheckURL string
}

// BotConfig contient les paramètres transmis au cœur du bot.
type BotConfig struct {
	ExchangeName  string
	Pair          string
	CheckInterval time.Duration
	WebPort       string
	// HealthcheckURL : URL « dead-man's switch » pingée à chaque price-check.
	// Vide = désactivé. Si les pings cessent, le service distant alerte.
	HealthcheckURL string
}

// Load lit la configuration depuis les variables d'environnement.
// Toutes les variables ont des valeurs par défaut utilisables en développement.
func Load() AppConfig {
	checkIntervalMins, err := strconv.Atoi(getenv("CHECK_INTERVAL_MINUTES", "5"))
	if err != nil || checkIntervalMins <= 0 {
		checkIntervalMins = 5
	}

	return AppConfig{
		ExchangeName:   getenv("EXCHANGE", "mexc"),
		TradingPair:    getenv("TRADING_PAIR", "BTC/USDC"),
		CheckInterval:  time.Duration(checkIntervalMins) * time.Minute,
		DBPath:         getenv("DB_PATH", "db/bot.db"),
		LogLevel:       strings.ToLower(getenv("LOG_LEVEL", "info")),
		LogFile:        os.Getenv("LOG_FILE"),
		WebPort:        getenv("WEB_PORT", ":8080"),
		HealthcheckURL: os.Getenv("HEALTHCHECK_URL"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// GetLogLevel retourne le niveau de log configuré.
func (c AppConfig) GetLogLevel() string { return c.LogLevel }

// GetLogFile retourne le chemin du fichier de log (vide = stdout uniquement).
func (c AppConfig) GetLogFile() string { return c.LogFile }

// ToBotConfig convertit AppConfig en BotConfig.
func (c AppConfig) ToBotConfig() BotConfig {
	return BotConfig{
		ExchangeName:   c.ExchangeName,
		Pair:           c.TradingPair,
		CheckInterval:  c.CheckInterval,
		WebPort:        c.WebPort,
		HealthcheckURL: c.HealthcheckURL,
	}
}
