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
	// MakerFee : taux maker imposé (fraction), lu depuis MAKER_FEE_PCT. nil = lire
	// le tarif réel du compte sur l'exchange.
	MakerFee *float64

	// Relay de notifications mobile. RelayURL vide = désactivé.
	RelayURL             string
	RelayToken           string
	RelayInstance        string
	RelayInterval        time.Duration
	RelayBalanceInterval time.Duration
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
	// MakerFee : taux maker imposé (fraction). nil = tarif du compte lu sur l'exchange.
	MakerFee *float64
}

// Load lit la configuration depuis les variables d'environnement.
// Toutes les variables ont des valeurs par défaut utilisables en développement.
func Load() AppConfig {
	checkIntervalMins, err := strconv.Atoi(getenv("CHECK_INTERVAL_MINUTES", "5"))
	if err != nil || checkIntervalMins <= 0 {
		checkIntervalMins = 5
	}

	exchange := getenv("EXCHANGE", "okx")

	return AppConfig{
		ExchangeName:   exchange,
		TradingPair:    getenv("TRADING_PAIR", "BTC/USDC"),
		CheckInterval:  time.Duration(checkIntervalMins) * time.Minute,
		DBPath:         getenv("DB_PATH", "db/bot.db"),
		LogLevel:       strings.ToLower(getenv("LOG_LEVEL", "info")),
		LogFile:        os.Getenv("LOG_FILE"),
		WebPort:        getenv("WEB_PORT", ":8080"),
		HealthcheckURL: os.Getenv("HEALTHCHECK_URL"),
		MakerFee:       getenvPercent("MAKER_FEE_PCT"),

		RelayURL:   os.Getenv("RELAY_URL"),
		RelayToken: os.Getenv("RELAY_TOKEN"),
		// L'instance identifie ce bot auprès d'un relay qui peut en servir
		// plusieurs ; par défaut le nom de l'exchange suffit.
		RelayInstance: getenv("RELAY_INSTANCE", exchange),
		RelayInterval: time.Duration(getenvInt("RELAY_INTERVAL_MINUTES", 1)) * time.Minute,
		// La valorisation du portefeuille interroge l'exchange : espacée davantage
		// que le reste du snapshot pour ne pas saturer les rate limits.
		RelayBalanceInterval: time.Duration(getenvInt("RELAY_BALANCE_INTERVAL_MINUTES", 15)) * time.Minute,
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getenvInt lit un entier strictement positif, en retombant sur fallback si la
// variable est absente, illisible ou nulle.
func getenvInt(key string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// getenvPercent lit un pourcentage positif ou nul (« 0.08 » = 0,08 %) et le rend en
// fraction. nil si la variable est absente ou illisible.
func getenvPercent(key string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	if err != nil || v < 0 {
		return nil
	}
	f := v / 100.0
	return &f
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
		MakerFee:       c.MakerFee,
	}
}
