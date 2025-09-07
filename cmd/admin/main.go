package main

import (
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"
	"time"
)

func main() {
	projectRoot, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)

	var (
		botDir  = flag.String("root", ".", "Path to the bot root directory")
		command = flag.String("cmd", "stats", "Command to execute: stats, positions, orders, export")
		format  = flag.String("format", "table", "Output format: table, json")
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

	err = logger.InitLogger(fileConfig.GetLogLevel(), fileConfig.GetLogFile())
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	db, err := database.NewDB(fileConfig.Database.Path)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Retourne au dossier racine par défaut
	err = os.Chdir(projectRoot)
	if err != nil {
		log.Fatalf("Failed to change directory back to %s: %v", projectRoot, err)
	}

	switch *command {
	case "stats":
		showStats(db, *format)
	case "positions":
		showPositions(db, *format)
	case "orders":
		showOrders(db, *format)
	case "export":
		exportData(db)
	default:
		fmt.Printf("Unknown command: %s\n", *command)
		fmt.Println("Available commands: stats, positions, orders, export")
		os.Exit(1)
	}
}

func showStats(db *database.DB, format string) {
	stats, err := db.GetStats()
	if err != nil {
		logger.Fatalf("Failed to get stats: %v", err)
	}

	switch format {
	case "json":
		data, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Println("=== Bot Statistics ===")
		for key, value := range stats {
			fmt.Printf("%s: %v\n", key, value)
		}
	}
}

func showPositions(db *database.DB, format string) {
	positions, err := db.GetAllPositions()
	if err != nil {
		logger.Fatalf("Failed to get positions: %v", err)
	}

	switch format {
	case "json":
		data, _ := json.MarshalIndent(positions, "", "  ")
		fmt.Println(string(data))
	default:
		if len(positions) == 0 {
			fmt.Println("No active positions")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tPrice\tAmount\tValue\tAge\tCreated At")
		fmt.Fprintln(w, "---\t-----\t------\t-----\t---\t----------")

		for _, pos := range positions {
			age := time.Since(pos.CreatedAt)
			value := pos.Price * pos.Amount
			fmt.Fprintf(w, "%d\t%.2f\t%.6f\t%.2f\t%s\t%s\n",
				pos.ID, pos.Price, pos.Amount, value,
				formatDuration(age), pos.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		w.Flush()
	}
}

func showOrders(db *database.DB, format string) {
	orders, err := db.GetPendingOrders()
	if err != nil {
		logger.Fatalf("Failed to get orders: %v", err)
	}

	switch format {
	case "json":
		data, _ := json.MarshalIndent(orders, "", "  ")
		fmt.Println(string(data))
	default:
		if len(orders) == 0 {
			fmt.Println("No pending orders")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tExternal ID\tSide\tPrice\tAmount\tPosition ID\tAge\tCreated At")
		fmt.Fprintln(w, "---\t-----------\t----\t-----\t------\t-----------\t---\t----------")

		for _, order := range orders {
			age := time.Since(order.CreatedAt)
			posID := "N/A"
			if order.PositionID != nil {
				posID = fmt.Sprintf("%d", *order.PositionID)
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%.2f\t%.6f\t%s\t%s\t%s\n",
				order.ID, order.ExternalID, order.Side, order.Price, order.Amount, posID,
				formatDuration(age), order.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		w.Flush()
	}
}

func exportData(db *database.DB) {
	// Créer un export complet
	export := make(map[string]interface{})

	positions, err := db.GetAllPositions()
	if err != nil {
		logger.Warnf("Warning: Failed to export positions: %v", err)
	} else {
		export["positions"] = positions
	}

	// Pour l'export, on récupère aussi les ordres pending
	orders, err := db.GetPendingOrders()
	if err != nil {
		logger.Warnf("Warning: Failed to export orders: %v", err)
	} else {
		export["pending_orders"] = orders
	}

	stats, err := db.GetStats()
	if err != nil {
		logger.Warnf("Warning: Failed to export stats: %v", err)
	} else {
		export["statistics"] = stats
	}

	export["exported_at"] = time.Now()

	filename := fmt.Sprintf("bot_export_%s.json", time.Now().Format("2006-01-02_15-04-05"))
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		logger.Fatalf("Failed to marshal export data: %v", err)
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		logger.Fatalf("Failed to write export file: %v", err)
	}

	fmt.Printf("Data exported to: %s\n", filename)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
