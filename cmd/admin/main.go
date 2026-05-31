package main

import (
	"bot/internal/core/database"
	"bot/internal/loader"
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
	log.SetOutput(os.Stdout)

	var (
		botDir  = flag.String("root", ".", "Répertoire racine de l'instance du bot")
		command = flag.String("cmd", "stats", "Commande : stats, cycles, orders, export")
		format  = flag.String("format", "table", "Format de sortie : table, json")
	)
	flag.Parse()

	if *botDir != "." {
		if err := os.Chdir(*botDir); err != nil {
			log.Fatalf("Impossible de changer de répertoire vers %s : %v", *botDir, err)
		}
	}

	_, db, err := loader.LoadConfig()
	if err != nil {
		log.Fatalf("Échec du chargement de la configuration : %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Fatalf("Échec de la fermeture de la DB : %v", err)
		}
	}()

	switch *command {
	case "stats":
		showStats(db, *format)
	case "cycles":
		showCycles(db, *format)
	case "orders":
		showOrders(db, *format)
	case "export":
		exportData(db)
	default:
		fmt.Printf("Commande inconnue : %s\n", *command)
		fmt.Println("Commandes disponibles : stats, cycles, orders, export")
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

func showCycles(db *database.DB, format string) {
	cycles, err := db.GetAllCycles()
	if err != nil {
		logger.Fatalf("Failed to get cycles: %v", err)
	}

	switch format {
	case "json":
		data, _ := json.MarshalIndent(cycles, "", "  ")
		fmt.Println(string(data))
	default:
		if len(cycles) == 0 {
			fmt.Println("No cycles")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tStatus\tAmount\tBuy Price\tSell Price\tProfit\tAge\tCreated At")
		fmt.Fprintln(w, "--\t------\t------\t---------\t----------\t------\t---\t---------")

		for _, cycle := range cycles {
			age := time.Since(cycle.CreatedAt)

			var sellPrice string
			var profit string
			if cycle.SellOrder != nil {
				sellPrice = fmt.Sprintf("%.2f", cycle.SellOrder.Price)
				profit = fmt.Sprintf("%.2f", *cycle.Profit)
			} else {
				sellPrice = ""
				profit = ""
			}

			fmt.Fprintf(w, "%d\t%s\t%.2f\t%.2f\t%s\t%s\t%s\t%s\n",
				cycle.ID, cycle.Status, cycle.BuyOrder.Amount, cycle.BuyOrder.Price, sellPrice, profit,
				formatDuration(age), cycle.CreatedAt.Format("2006-01-02 15:04:05"))
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
		fmt.Fprintln(w, "ID\tExternal ID\tSide\tPrice\tAmount\tAge\tCreated At")
		fmt.Fprintln(w, "--\t-----------\t----\t-----\t------\t---\t----------")

		for _, order := range orders {
			age := time.Since(order.CreatedAt)
			fmt.Fprintf(w, "%d\t%s\t%s\t%.2f\t%.6f\t%s\t%s\n",
				order.ID, order.ExternalID, order.Side, order.Price, order.Amount,
				formatDuration(age), order.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		w.Flush()
	}
}

func exportData(db *database.DB) {
	// Créer un export complet
	export := make(map[string]interface{})

	// positions, err := db.GetAllPositions()
	// if err != nil {
	// 	logger.Warnf("Warning: Failed to export positions: %v", err)
	// } else {
	// 	export["positions"] = positions
	// }

	cycles, err := db.GetAllCycles()
	if err != nil {
		logger.Warnf("Warning: Failed to export cycles: %v", err)
	} else {
		export["cycles"] = cycles
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
