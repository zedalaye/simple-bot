package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const CONFIG_FILENAME = "config.yml"

type FileConfig struct {
	Exchange struct {
		Name string `yaml:"name" json:"name"`
		// Note: API credentials should come from environment variables for security
	} `yaml:"exchange" json:"exchange"`

	Trading struct {
		Pair string `yaml:"pair" json:"pair"`
		// Buy
		QuoteAmount   float64 `yaml:"quote_amount" json:"quote_amount"`
		MaxBuysPerDay int     `yaml:"max_buys_per_day" json:"max_buys_per_day"`
		RSIPeriod     int     `yaml:"rsi_period" json:"rsi_period"`
		RSIThreshold  float64 `yaml:"rsi_threshold" json:"rsi_threshold"`
		// Sell
		ProfitTarget         float64 `yaml:"profit_target" json:"profit_target"`
		VolatilityPeriod     int     `yaml:"volatility_period" json:"volatility_period"`
		VolatilityAdjustment float64 `yaml:"volatility_adjustment" json:"volatility_adjustment"`
		TrailingStopDelta    float64 `yaml:"trailing_stop_delta" json:"trailing_stop_delta"`
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

	Web struct {
		Port string `yaml:"port" json:"port"`
	} `yaml:"web" json:"web"`
}

func LoadConfig() (*FileConfig, error) {
	// Configuration par défaut
	defaultConfig := &FileConfig{
		Exchange: struct {
			Name string `yaml:"name" json:"name"`
		}{
			Name: "mexc",
		},
		Trading: struct {
			Pair string `yaml:"pair" json:"pair"`
			// Buy
			QuoteAmount   float64 `yaml:"quote_amount" json:"quote_amount"`
			MaxBuysPerDay int     `yaml:"max_buys_per_day" json:"max_buys_per_day"`
			RSIPeriod     int     `yaml:"rsi_period" json:"rsi_period"`
			RSIThreshold  float64 `yaml:"rsi_threshold" json:"rsi_threshold"`
			// Sell
			ProfitTarget         float64 `yaml:"profit_target" json:"profit_target"`
			VolatilityPeriod     int     `yaml:"volatility_period" json:"volatility_period"`
			VolatilityAdjustment float64 `yaml:"volatility_adjustment" json:"volatility_adjustment"`
			TrailingStopDelta    float64 `yaml:"trailing_stop_delta" json:"trailing_stop_delta"`
		}{
			Pair: "BTC/USDC",
			// Buy
			QuoteAmount:   50.0,
			MaxBuysPerDay: 4,
			RSIPeriod:     14,
			RSIThreshold:  70.0,
			// Sell
			ProfitTarget:         2.0,
			VolatilityPeriod:     7,
			VolatilityAdjustment: 50.0,
			TrailingStopDelta:    0.1,
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
			Path: "db/bot.db",
		},
		Logging: struct {
			Level string `yaml:"level" json:"level"`
			File  string `yaml:"file,omitempty" json:"file,omitempty"`
		}{
			Level: "info",
			File:  "", // Empty means stdout only
		},
		Web: struct {
			Port string `yaml:"port" json:"port"`
		}{
			Port: ":8080",
		},
	}

	// Si le fichier n'existe pas, créer la configuration par défaut
	if _, err := os.Stat(CONFIG_FILENAME); os.IsNotExist(err) {
		return createDefaultConfigFile(CONFIG_FILENAME, defaultConfig)
	}

	// Charger le fichier de configuration
	data, err := os.ReadFile(CONFIG_FILENAME)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config FileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config file: %w", err)
	}

	// Validation basique
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

func createDefaultConfigFile(filename string, defaultConfig *FileConfig) (*FileConfig, error) {
	_, err := yaml.Marshal(defaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal default config: %w", err)
	}

	// Ajouter des commentaires utiles
	configWithComments := `# Trading Bot Configuration
# 
# Exchange configuration
exchange:
  name: mexc                     # Supported: mexc, hyperliquid, binance, etc.

# Trading parameters
trading:
  pair: BTC/USDC                 # Trading pair

  quote_amount: 50.0             # Amount in quote currency (USDC) per buy order
  max_buys_per_day: 4            # Maximum number of buy orders per 24 hours (1-24)
  rsi_period: 14                 # Days of data (4h candles) for RSI calculation (14 is standard)
  rsi_threshold: 70.0            # RSI threshold (> 70 = no buy signal), 100 to disable

  profit_target: 2.0             # Profit target in percentage (2.0 = 2%) to trigger sell logic
  volatility_period: 7           # Days of data for volatility calculation
  volatility_adjustment: 50.0    # Profit threshold adjustment percentage per 1% volatility (50.0 = 50% adjustment per 1% volatility)
  trailing_stop_delta: 0.1       # Trailing Stop Delta in % (sell when the price drop under 0.1% < of MaxPrice)

# Timing intervals
intervals:
  buy_interval_hours: 4          # Hours between buy attempts
  check_interval_minutes: 60     # Minutes between price/order checks

# Database configuration
database:
  path: db/bot.db                # SQLite database file path

# Logging configuration
logging:
  level: info                    # Levels: debug, info, warn, error
  file: ""                       # Optional log file (empty = stdout only)

# Web server configuration
web:
  port: ":8080"                  # Port for the web interface

# Note: Set API_KEY and API_SECRET environment variables for exchange access in .env
`

	err = os.WriteFile(filename, []byte(configWithComments), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create default config file: %w", err)
	}

	fmt.Printf("Created default configuration file: %s\n", filename)
	fmt.Println("Please review and adjust the settings, then restart the bot.")
	fmt.Println("Remember to make exchange related secrets (like API_KEY and API_SECRET) available through environment variables or a .env file")

	return defaultConfig, nil
}

func validateConfig(config *FileConfig) error {
	if config.Trading.QuoteAmount <= 0 {
		return fmt.Errorf("trading.quote_amount must be positive")
	}
	if config.Trading.RSIPeriod < 1 {
		return fmt.Errorf("trading.rsi_period must be at least 1")
	}
	if config.Trading.RSIThreshold < 0 || config.Trading.RSIThreshold > 100 {
		return fmt.Errorf("trading.rsi_threshold must be between 0 and 100")
	}
	if config.Trading.VolatilityPeriod < 1 {
		return fmt.Errorf("trading.volatility_period must be at least 1")
	}
	if config.Trading.VolatilityAdjustment < 0 {
		return fmt.Errorf("trading.volatility_adjustment cannot be negative")
	}
	if config.Trading.ProfitTarget <= 0 {
		return fmt.Errorf("trading.profit_threshold must be greater than 0")
	}
	if config.Trading.TrailingStopDelta <= 0 || config.Trading.TrailingStopDelta > 100 {
		return fmt.Errorf("trading.rsi_threshold must be greater than 0 and lower or equal than 100")
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
	if config.Web.Port == "" {
		return fmt.Errorf("web.port cannot be empty")
	}
	if config.Trading.MaxBuysPerDay < 1 || config.Trading.MaxBuysPerDay > 24*60 {
		return fmt.Errorf("trading.max_buys_per_day must be between 1 and 1440")
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
	ExchangeName string
	Pair         string
	// Scheduling
	BuyInterval   time.Duration
	CheckInterval time.Duration
	// Buy
	QuoteAmount   float64
	MaxBuysPerDay int
	RSIPeriod     int
	RSIThreshold  float64
	// Sell
	ProfitTarget         float64
	VolatilityPeriod     int
	VolatilityAdjustment float64
	TrailingStopDelta    float64
	// Web UI
	WebPort string
}

func envFileExists(relFileName string) (string, bool) {
	if path, err := filepath.Abs(relFileName); err == nil {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

func (fc *FileConfig) EnvFilePaths() []string {
	var envFiles []string

	if path, ok := envFileExists("./.env"); ok {
		envFiles = append(envFiles, path)
	}
	if path, ok := envFileExists("../.env.tg"); ok {
		envFiles = append(envFiles, path)
	}
	if path, ok := envFileExists("../.env"); ok {
		envFiles = append(envFiles, path)
	}

	return envFiles
}

func (fc *FileConfig) ToBotConfig() BotConfig {
	return BotConfig{
		ExchangeName: fc.Exchange.Name,
		Pair:         fc.Trading.Pair,
		// Scheduling
		BuyInterval:   time.Duration(fc.Intervals.BuyIntervalHours) * time.Hour,
		CheckInterval: time.Duration(fc.Intervals.CheckIntervalMins) * time.Minute,
		// Buy
		QuoteAmount:   fc.Trading.QuoteAmount,
		MaxBuysPerDay: fc.Trading.MaxBuysPerDay,
		RSIPeriod:     fc.Trading.RSIPeriod,
		RSIThreshold:  fc.Trading.RSIThreshold,
		// Sell
		ProfitTarget:         fc.Trading.ProfitTarget,
		VolatilityPeriod:     fc.Trading.VolatilityPeriod,
		VolatilityAdjustment: fc.Trading.VolatilityAdjustment,
		TrailingStopDelta:    fc.Trading.TrailingStopDelta,
		// Web UI
		WebPort: fc.Web.Port,
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
