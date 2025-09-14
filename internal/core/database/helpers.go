package database

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// Helper function to build an Order from scan result
func (db *DB) buildOrderFromScan(scanResult OrderScanResult) *Order {
	if !scanResult.ID.Valid {
		return nil
	}

	order := &Order{
		ID: int(scanResult.ID.Int64),
	}

	if scanResult.StrategyID.Valid {
		id := int(scanResult.StrategyID.Int64)
		order.StrategyID = &id
	}
	if scanResult.ExternalID.Valid {
		order.ExternalID = scanResult.ExternalID.String
	}
	if scanResult.Side.Valid {
		order.Side = OrderSide(scanResult.Side.String)
	}
	if scanResult.Amount.Valid {
		if val, err := strconv.ParseFloat(scanResult.Amount.String, 64); err == nil {
			order.Amount = val
		}
	}
	if scanResult.Price.Valid {
		if val, err := strconv.ParseFloat(scanResult.Price.String, 64); err == nil {
			order.Price = val
		}
	}
	if scanResult.Fees.Valid {
		if val, err := strconv.ParseFloat(scanResult.Fees.String, 64); err == nil {
			order.Fees = val
		}
	}
	if scanResult.Status.Valid {
		order.Status = OrderStatus(scanResult.Status.String)
	}
	if scanResult.CreatedAt.Valid {
		order.CreatedAt = scanResult.CreatedAt.Time
	}
	if scanResult.UpdatedAt.Valid {
		order.UpdatedAt = scanResult.UpdatedAt.Time
	}

	return order
}

// Helper function to build a CycleEnhanced from orders
func (db *DB) buildCycleEnhancedFromOrders(id int, targetPrice, maxPrice float64, createdAt, updatedAt time.Time, buyOrder *Order, sellOrder *Order) (*CycleEnhanced, error) {
	if buyOrder == nil {
		return nil, fmt.Errorf("buy order cannot be nil")
	}

	cycle := &CycleEnhanced{
		Cycle: Cycle{
			ID:          id,
			BuyOrder:    *buyOrder,
			SellOrder:   sellOrder,
			MaxPrice:    maxPrice,
			TargetPrice: targetPrice,
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		},
	}

	// Récupérer le StrategyID depuis le buy order
	if buyOrder.StrategyID == nil {
		return nil, fmt.Errorf("buyOrder.StrategyID should not be nil")
	}
	cycle.StrategyID = *buyOrder.StrategyID

	// Logique de statut et profit (réutilisée de buildCycleEnhanced)
	if cycle.SellOrder == nil {
		switch cycle.BuyOrder.Status {
		case Pending, Cancelled:
			cycle.Status = New
		case Filled:
			cycle.Status = Open
		}
	} else {
		switch cycle.SellOrder.Status {
		case Pending, Cancelled:
			cycle.Status = Running
		case Filled:
			cycle.Status = Completed
		}

		profit := (cycle.SellOrder.Price - cycle.BuyOrder.Price) * cycle.BuyOrder.Amount
		profit -= cycle.BuyOrder.Fees
		profit -= cycle.SellOrder.Fees
		cycle.Profit = &profit
	}

	// Calcul de durée (réutilisée de buildCycleEnhanced)
	var duration time.Duration
	if cycle.SellOrder != nil {
		duration = cycle.SellOrder.CreatedAt.Sub(cycle.CreatedAt)
	} else {
		duration = time.Since(cycle.BuyOrder.CreatedAt)
	}
	cycle.Duration = formatDuration(duration)

	return cycle, nil
}

// Helper function to scan a cycle row
func (db *DB) scanCycleRow(rows *sql.Rows) (*CycleScanResult, error) {
	var result CycleScanResult

	err := rows.Scan(
		&result.ID, &result.TargetPrice, &result.MaxPrice, &result.CreatedAt, &result.UpdatedAt,
		// Buy Order
		&result.BuyOrder.ID, &result.BuyOrder.StrategyID, &result.BuyOrder.ExternalID,
		&result.BuyOrder.Side, &result.BuyOrder.Amount, &result.BuyOrder.Price,
		&result.BuyOrder.Fees, &result.BuyOrder.Status, &result.BuyOrder.CreatedAt, &result.BuyOrder.UpdatedAt,
		// Sell Order (nullable)
		&result.SellOrder.ID, &result.SellOrder.StrategyID, &result.SellOrder.ExternalID,
		&result.SellOrder.Side, &result.SellOrder.Amount, &result.SellOrder.Price,
		&result.SellOrder.Fees, &result.SellOrder.Status, &result.SellOrder.CreatedAt, &result.SellOrder.UpdatedAt,
	)

	return &result, err
}

// Helper function to scan a single order row (for direct order queries)
func (db *DB) scanSingleOrderRow(rows *sql.Rows) (*Order, error) {
	var order Order
	var strategyId sql.NullInt64

	err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
		&order.Status, &strategyId, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to scan order: %w", err)
	}

	if strategyId.Valid {
		id := int(strategyId.Int64)
		order.StrategyID = &id
	}

	return &order, nil
}

// Common function to execute order queries
func (db *DB) executeOrderQuery(query string, args ...interface{}) ([]Order, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute order query: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		order, err := db.scanSingleOrderRow(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order row: %w", err)
		}
		orders = append(orders, *order)
	}

	return orders, nil
}

// Common function to execute cycle queries
func (db *DB) executeCycleQuery(query string, args ...interface{}) ([]CycleEnhanced, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute cycle query: %w", err)
	}
	defer rows.Close()

	var cycles []CycleEnhanced
	for rows.Next() {
		scanResult, err := db.scanCycleRow(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cycle row: %w", err)
		}

		// Construire les orders
		buyOrder := db.buildOrderFromScan(scanResult.BuyOrder)
		sellOrder := db.buildOrderFromScan(scanResult.SellOrder)

		// Construire le cycle enhanced
		cycle, err := db.buildCycleEnhancedFromOrders(
			scanResult.ID,
			scanResult.TargetPrice,
			scanResult.MaxPrice,
			scanResult.CreatedAt,
			scanResult.UpdatedAt,
			buyOrder,
			sellOrder,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to build cycle enhanced: %w", err)
		}

		cycles = append(cycles, *cycle)
	}

	return cycles, nil
}

// Helper pour formater la durée
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d sec", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
	} else {
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%d j %d h", days, hours)
	}
}
