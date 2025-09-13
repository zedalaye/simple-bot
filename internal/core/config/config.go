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
		Pair string `yaml:"pair" json:"pair"` // Default trading pair for this exchange
	} `yaml:"trading" json:"trading"`

	Intervals struct {
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
		}{
			Pair: "BTC/USDC",
		},
		Intervals: struct {
			CheckIntervalMins int `yaml:"check_interval_minutes" json:"check_interval_minutes"`
		}{
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

# Trading parameters (strategy-specific parameters are configured per strategy)
trading:
	 pair: BTC/USDC                 # Default trading pair for this exchange

# Timing intervals
intervals:
	 check_interval_minutes: 5      # Minutes between price/order checks

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
# Note: Trading parameters (RSI, profit targets, etc.) are now configured per strategy
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
	if config.Exchange.Name == "" {
		return fmt.Errorf("exchange.name cannot be empty")
	}
	if config.Trading.Pair == "" {
		return fmt.Errorf("trading.pair cannot be empty")
	}
	if config.Intervals.CheckIntervalMins <= 0 {
		return fmt.Errorf("intervals.check_interval_minutes must be > 0")
	}
	if config.Database.Path == "" {
		return fmt.Errorf("database.path cannot be empty")
	}
	if config.Web.Port == "" {
		return fmt.Errorf("web.port cannot be empty")
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
	ExchangeName  string
	Pair          string
	CheckInterval time.Duration
	WebPort       string
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
		ExchangeName:  fc.Exchange.Name,
		Pair:          fc.Trading.Pair,
		CheckInterval: time.Duration(fc.Intervals.CheckIntervalMins) * time.Minute,
		WebPort:       fc.Web.Port,
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
