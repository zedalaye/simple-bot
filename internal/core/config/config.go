package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type FileConfig struct {
	Exchange struct {
		Name string `yaml:"name" json:"name"`
		// Note: API credentials should come from environment variables for security
	} `yaml:"exchange" json:"exchange"`

	Trading struct {
		Pair            string  `yaml:"pair" json:"pair"`
		QuoteAmount     float64 `yaml:"quote_amount" json:"quote_amount"`
		PriceOffset     float64 `yaml:"price_offset" json:"price_offset"`
		ProfitThreshold float64 `yaml:"profit_threshold" json:"profit_threshold"`
		OrderTTL        int64   `yaml:"order_ttl" json:"order_ttl"`
	} `yaml:"trading" json:"trading"`

	Intervals struct {
		BuyIntervalHours  int `yaml:"buy_interval_hours" json:"buy_interval_hours"`
		CheckIntervalMins int `yaml:"check_interval_minutes" json:"check_interval_minutes"`
	} `yaml:"intervals" json:"intervals"`

	Database struct {
		Path string `yaml:"path" json:"path"`
	} `yaml:"database" json:"database"`

	Logging struct {
		Level string `yaml:"level" json:"level"`
		File  string `yaml:"file,omitempty" json:"file,omitempty"`
	} `yaml:"logging" json:"logging"`
}

func LoadConfig(filename string) (*FileConfig, error) {
	// Configuration par défaut
	defaultConfig := &FileConfig{
		Exchange: struct {
			Name string `yaml:"name" json:"name"`
		}{
			Name: "mexc",
		},
		Trading: struct {
			Pair            string  `yaml:"pair" json:"pair"`
			QuoteAmount     float64 `yaml:"quote_amount" json:"quote_amount"`
			PriceOffset     float64 `yaml:"price_offset" json:"price_offset"`
			ProfitThreshold float64 `yaml:"profit_threshold" json:"profit_threshold"`
			OrderTTL        int64   `yaml:"order_ttl" json:"order_ttl"`
		}{
			Pair:            "BTC/USDC",
			QuoteAmount:     50.0,
			PriceOffset:     200.0,
			ProfitThreshold: 1.015,
			OrderTTL:        18,
		},
		Intervals: struct {
			BuyIntervalHours  int `yaml:"buy_interval_hours" json:"buy_interval_hours"`
			CheckIntervalMins int `yaml:"check_interval_minutes" json:"check_interval_minutes"`
		}{
			BuyIntervalHours:  4,
			CheckIntervalMins: 5,
		},
		Database: struct {
			Path string `yaml:"path" json:"path"`
		}{
			Path: "bot.db",
		},
		Logging: struct {
			Level string `yaml:"level" json:"level"`
			File  string `yaml:"file,omitempty" json:"file,omitempty"`
		}{
			Level: "info",
			File:  "", // Empty means stdout only
		},
	}

	// Si le fichier n'existe pas, créer la configuration par défaut
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return createDefaultConfigFile(filename, defaultConfig)
	}

	// Charger le fichier de configuration
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config FileConfig

	// Déterminer le format basé sur l'extension
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".yml", ".yaml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config file: %w", err)
		}
	case ".json":
		// Fallback JSON support (vous devrez importer encoding/json)
		return nil, fmt.Errorf("JSON support not implemented in this version, please use YAML")
	default:
		return nil, fmt.Errorf("unsupported config file format: %s (use .yml or .yaml)", ext)
	}

	// Validation basique
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

func createDefaultConfigFile(filename string, defaultConfig *FileConfig) (*FileConfig, error) {
	// Forcer l'extension YAML si pas spécifiée
	if !strings.HasSuffix(strings.ToLower(filename), ".yml") &&
		!strings.HasSuffix(strings.ToLower(filename), ".yaml") {
		filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".yml"
	}

	_, err := yaml.Marshal(defaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal default config: %w", err)
	}

	// Ajouter des commentaires utiles
	configWithComments := `# Trading Bot Configuration
# 
# Exchange configuration
exchange:
  name: mexc  # Supported: mexc, binance, etc.

# Trading parameters
trading:
  pair: BTC/USDC           # Trading pair
  quote_amount: 50.0       # Amount in quote currency (USDC) per buy order
  price_offset: 200.0      # Price difference for limit orders (USDC)
  profit_threshold: 1.015  # Profit threshold (1.5% = 1.015)
  order_ttl: 18            # Hours

# Timing intervals
intervals:
  buy_interval_hours: 4     # Hours between buy attempts
  check_interval_minutes: 5 # Minutes between price/order checks

# Database configuration
database:
  path: bot.db             # SQLite database file path

# Logging configuration
logging:
  level: info              # Levels: debug, info, warn, error
  file: ""                 # Optional log file (empty = stdout only)

# Note: Set API_KEY and API_SECRET environment variables for exchange access
`

	err = os.WriteFile(filename, []byte(configWithComments), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create default config file: %w", err)
	}

	fmt.Printf("Created default configuration file: %s\n", filename)
	fmt.Println("Please review and adjust the settings, then restart the bot.")
	fmt.Println("Remember to set API_KEY and API_SECRET environment variables.")

	return defaultConfig, nil
}

func validateConfig(config *FileConfig) error {
	if config.Trading.QuoteAmount <= 0 {
		return fmt.Errorf("trading.quote_amount must be positive")
	}
	if config.Trading.PriceOffset < 0 {
		return fmt.Errorf("trading.price_offset must be non-negative")
	}
	if config.Trading.ProfitThreshold <= 1.0 {
		return fmt.Errorf("trading.profit_threshold must be greater than 1.0")
	}
	if config.Trading.OrderTTL < 1 {
		return fmt.Errorf("trading.order_ttl must be greater or equal than 1")
	}
	if config.Intervals.BuyIntervalHours <= 0 {
		return fmt.Errorf("intervals.buy_interval_hours must be positive")
	}
	if config.Intervals.CheckIntervalMins <= 0 {
		return fmt.Errorf("intervals.check_interval_minutes must be positive")
	}
	if config.Exchange.Name == "" {
		return fmt.Errorf("exchange.name cannot be empty")
	}
	if config.Database.Path == "" {
		return fmt.Errorf("database.path cannot be empty")
	}

	// Validation des niveaux de log
	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLogLevels[strings.ToLower(config.Logging.Level)] {
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error")
	}

	return nil
}

type BotConfig struct {
	Pair            string
	QuoteAmount     float64
	PriceOffset     float64
	ProfitThreshold float64
	OrderTTL        time.Duration
	BuyInterval     time.Duration
	CheckInterval   time.Duration
}

func (fc *FileConfig) ToBotConfig() BotConfig {
	return BotConfig{
		Pair:            fc.Trading.Pair,
		QuoteAmount:     fc.Trading.QuoteAmount,
		PriceOffset:     fc.Trading.PriceOffset,
		ProfitThreshold: fc.Trading.ProfitThreshold,
		OrderTTL:        time.Duration(fc.Trading.OrderTTL) * time.Hour,
		BuyInterval:     time.Duration(fc.Intervals.BuyIntervalHours) * time.Hour,
		CheckInterval:   time.Duration(fc.Intervals.CheckIntervalMins) * time.Minute,
	}
}

// GetLogLevel returns the configured log level
func (fc *FileConfig) GetLogLevel() string {
	return strings.ToLower(fc.Logging.Level)
}

// GetLogFile returns the configured log file path (empty string means stdout only)
func (fc *FileConfig) GetLogFile() string {
	return fc.Logging.File
}
