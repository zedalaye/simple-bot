package main

import (
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
)

func main() {
	projectRoot, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)

	var (
		botDir = flag.String("root", ".", "Path to the bot directory")
	)
	flag.Parse()

	// Changer le répertoire de travail si nécessaire
	if *botDir != "." {
		err := os.Chdir(*botDir)
		if err != nil {
			log.Fatalf("Failed to change directory to %s: %v", *botDir, err)
		}
	}

	// Load configuration
	fileConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	err = logger.InitLogger(fileConfig.GetLogLevel(), fileConfig.GetLogFile())
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Initialize database
	db, err := database.NewDB(fileConfig.Database.Path)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer func(db *database.DB) {
		err := db.Close()
		if err != nil {
			logger.Fatalf("Failed to close database: %v", err)
		}
	}(db)

	// Retourne au dossier racine par défaut
	err = os.Chdir(projectRoot)
	if err != nil {
		log.Fatalf("Failed to change directory back to %s: %v", projectRoot, err)
	}

	// Cycles may only be fixed once
	existingCycles, err := db.GetAllCycles()
	if err != nil {
		logger.Fatalf("Failed to get all cycles: %v", err)
	}
	if len(existingCycles) > 0 {
		logger.Infof("%d cycles have already been initialized", len(existingCycles))
		os.Exit(1)
	}

	// Get All Orders
	orders, err := db.GetAllOrders()
	if err != nil {
		logger.Fatalf("Failed to get all positions: %v", err)
	}

	// sort by id
	sort.Slice(orders, func(i, j int) bool {
		return orders[i].ID < orders[j].ID
	})

	// skip cancelled
	var nonCancelledOrders []database.Order
	for _, order := range orders {
		if order.Status != "CANCELLED" {
			nonCancelledOrders = append(nonCancelledOrders, order)
		}
	}

	// number "buy" positions chronologically
	buyPosition := 0
	for i, order := range nonCancelledOrders {
		if order.Side == "BUY" {
			buyPosition++
			buyPositionId := buyPosition
			nonCancelledOrders[i].PositionID = &buyPositionId

			err = db.UpdateOrderPosition(order.ID, buyPositionId)
			if err != nil {
				logger.Fatalf("Failed to update buy order position : %v", err)
			}
		}
	}

	// debug print
	for i, order := range nonCancelledOrders {
		positionID := 0
		if order.PositionID != nil {
			positionID = *order.PositionID
		}
		fmt.Printf("Order[%d]: %d, %s, %s, %d\n", i, order.ID, order.Side, order.Status, positionID)
	}

	// assemble cycles
	var cycles []database.Cycle
	for i := 1; i <= buyPosition; i++ {
		var buyOrder database.Order
		var sellOrder *database.Order

		for _, order := range nonCancelledOrders {
			if order.Side == "BUY" && *order.PositionID == i {
				buyOrder = order
			}
			if order.Side == "SELL" && *order.PositionID == i {
				sellOrder = &order
			}
		}

		cycleUpdatedAt := buyOrder.CreatedAt
		if sellOrder != nil {
			cycleUpdatedAt = sellOrder.CreatedAt
		}

		cycle := database.Cycle{
			BuyOrder:  buyOrder,
			SellOrder: sellOrder,
			CreatedAt: buyOrder.CreatedAt,
			UpdatedAt: cycleUpdatedAt,
		}
		cycles = append(cycles, cycle)
	}

	// debug print
	for i, cycle := range cycles {
		sellOrderStr := ""
		if cycle.SellOrder != nil {
			sellOrderId := (*cycle.SellOrder).ID
			sellOrderAmount := (*cycle.SellOrder).Amount
			sellOrderPrice := (*cycle.SellOrder).Price
			sellOrderStr = fmt.Sprintf("%d (%v x %v)", sellOrderId, sellOrderAmount, sellOrderPrice)
		} else {
			sellOrderStr = "NULL"
		}
		fmt.Printf("Cycle[%d]: %d (%v x %v), %s\n", i, cycle.BuyOrder.ID, cycle.BuyOrder.Amount, cycle.BuyOrder.Price, sellOrderStr)
	}

	// create cycles
	for _, cycle := range cycles {
		newCycle, err := db.CreateCycle(cycle.BuyOrder.ID, 1) // StrategyId = 1 (legacy)
		if err != nil {
			logger.Fatalf("Failed to create cycle: %v", err)
		}
		if cycle.SellOrder != nil {
			err = db.UpdateCycleSellOrder(newCycle.ID, (*cycle.SellOrder).ID)
			if err != nil {
				logger.Fatalf("Failed to update cycle sell order: %v", err)
			}
		}
		err = db.ForceCycleTimestamps(newCycle.ID, cycle.CreatedAt, cycle.UpdatedAt)
		if err != nil {
			logger.Fatalf("Failed to update cycle timestamps: %v", err)
		}
	}

	// check
	cycles, err = db.GetAllCycles()
	if err != nil {
		logger.Fatalf("Failed to get all cycles: %v", err)
	}

	// done !
	logger.Infof("Created %d cycles", len(cycles))
}
