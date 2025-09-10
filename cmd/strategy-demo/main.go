package main

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	var (
		dbPath = flag.String("db", "./db/bot.db", "Path to the database")
		action = flag.String("action", "list", "Action: list, create-examples, clear")
	)
	flag.Parse()

	// Initialize logger
	err := logger.InitLogger("info", "")
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Open database
	db, err := database.NewDB(*dbPath)
	if err != nil {
		logger.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	switch *action {
	case "list":
		listStrategies(db)
	case "create-examples":
		createExampleStrategies(db)
	case "clear":
		clearNonLegacyStrategies(db)
	default:
		fmt.Printf("Unknown action: %s\n", *action)
		fmt.Println("Available actions: list, create-examples, clear")
		os.Exit(1)
	}
}

func listStrategies(db *database.DB) {
	strategies, err := db.GetAllStrategies()
	if err != nil {
		logger.Fatalf("Failed to get strategies: %v", err)
	}

	fmt.Printf("\n📋 Current Strategies (%d total):\n", len(strategies))
	fmt.Println("==========================================")

	for _, strategy := range strategies {
		status := "🔴 Disabled"
		if strategy.Enabled {
			status = "🟢 Enabled"
		}

		fmt.Printf("ID: %d | %s\n", strategy.ID, status)
		fmt.Printf("Name: %s\n", strategy.Name)
		fmt.Printf("Algorithm: %s\n", strategy.AlgorithmName)
		fmt.Printf("Cron: %s\n", strategy.CronExpression)
		fmt.Printf("Quote Amount: %.2f USDC\n", strategy.QuoteAmount)
		if strategy.RSIThreshold != nil {
			fmt.Printf("RSI Threshold: %.2f\n", *strategy.RSIThreshold)
		}
		fmt.Printf("Profit Target: %.2f%%\n", strategy.ProfitTarget)
		fmt.Printf("Description: %s\n", strategy.Description)
		fmt.Println("------------------------------------------")
	}
}

func createExampleStrategies(db *database.DB) {
	fmt.Println("🚀 Creating example strategies...")

	examples := []struct {
		name         string
		description  string
		algorithm    string
		cron         string
		amount       float64
		rsiThresh    *float64
		rsiPeriod    *int
		profitTarget float64
	}{
		{
			name:         "Daily Conservative",
			description:  "1x/jour, RSI<30, +10% profit",
			algorithm:    "rsi_dca",
			cron:         "0 9 * * *", // Every day at 9am
			amount:       15.0,
			rsiThresh:    floatPtr(30.0),
			rsiPeriod:    intPtr(14),
			profitTarget: 10.0,
		},
		{
			name:         "Monthly Aggressive",
			description:  "1x/mois, RSI<30, +100% profit",
			algorithm:    "rsi_dca",
			cron:         "0 10 1 * *", // 1st of month at 10am
			amount:       50.0,
			rsiThresh:    floatPtr(30.0),
			rsiPeriod:    intPtr(14),
			profitTarget: 100.0,
		},
		{
			name:         "Scalping",
			description:  "4x/jour, RSI<70, +2% profit",
			algorithm:    "rsi_dca",
			cron:         "0 */6 * * *", // Every 6 hours
			amount:       25.0,
			rsiThresh:    floatPtr(70.0),
			rsiPeriod:    intPtr(14),
			profitTarget: 2.0,
		},
		{
			name:         "MACD Cross Demo",
			description:  "MACD crossover strategy demo",
			algorithm:    "macd_cross",
			cron:         "0 */4 * * *", // Every 4 hours
			amount:       30.0,
			rsiThresh:    nil, // MACD doesn't use RSI
			rsiPeriod:    nil,
			profitTarget: 3.0,
		},
	}

	for _, ex := range examples {
		err := db.CreateExampleStrategy(ex.name, ex.description, ex.algorithm, ex.cron,
			ex.amount, ex.profitTarget, ex.rsiThresh, ex.rsiPeriod)
		if err != nil {
			logger.Errorf("Failed to create strategy %s: %v", ex.name, err)
		} else {
			fmt.Printf("✅ Created strategy: %s\n", ex.name)
		}
	}

	fmt.Println("🎉 Example strategies created successfully!")
}

func clearNonLegacyStrategies(db *database.DB) {
	fmt.Println("🧹 Clearing non-legacy strategies...")

	// Delete all strategies except ID=1 (Legacy) using public method
	rowsAffected, err := db.DeleteNonLegacyStrategies()
	if err != nil {
		logger.Errorf("Failed to clear strategies: %v", err)
		return
	}

	fmt.Printf("✅ Deleted %d non-legacy strategies\n", rowsAffected)
}

// Helper functions
func floatPtr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}
