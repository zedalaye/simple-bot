package database

import (
	"database/sql"
	"fmt"
	"time"
)

// GetStats retrieves general database statistics
func (db *DB) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Nombre d'ordres en attente
	var pendingCount int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, Pending).Scan(&pendingCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending orders count: %w", err)
	}
	stats["pending_orders"] = pendingCount

	// Nombre d'ordres exécutés
	var filledCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, Filled).Scan(&filledCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get filled orders count: %w", err)
	}
	stats["filled_orders"] = filledCount

	// Nombre d'ordres annulés
	var cancelledCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, Cancelled).Scan(&cancelledCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get cancelled orders count: %w", err)
	}
	stats["cancelled_orders"] = cancelledCount

	// Nombre de cycles
	var cyclesCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM cycles`).Scan(&cyclesCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycles count: %w", err)
	}
	stats["cycles_count"] = cyclesCount

	// Nombre de cycles en cours
	var activeCyclesCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM cycles where sell_order_id is NULL`).Scan(&activeCyclesCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get active cycles count: %w", err)
	}
	stats["active_cycles_count"] = activeCyclesCount

	// Nombre de cycles complets
	var completedCyclesCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM cycles where sell_order_id is NOT NULL`).Scan(&completedCyclesCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get completed cycles count: %w", err)
	}
	stats["completed_cycles_count"] = completedCyclesCount

	// Calcul du profit moyen
	var avgProfit sql.NullFloat64
	err = db.conn.QueryRow(`
		SELECT AVG((so.price - bo.price) * bo.amount - bo.fees - so.fees)
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL
	`).Scan(&avgProfit)
	if err != nil {
		return nil, fmt.Errorf("failed to get average profit: %w", err)
	}
	if avgProfit.Valid {
		stats["average_profit"] = avgProfit.Float64
	} else {
		stats["average_profit"] = 0.0
	}

	// Calcul du profit total
	var totalProfit sql.NullFloat64
	err = db.conn.QueryRow(`
		SELECT SUM((so.price - bo.price) * bo.amount - bo.fees - so.fees)
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL
	`).Scan(&totalProfit)
	if err != nil {
		return nil, fmt.Errorf("failed to get total profit: %w", err)
	}
	if totalProfit.Valid {
		stats["total_profit"] = totalProfit.Float64
	} else {
		stats["total_profit"] = 0.0
	}

	return stats, nil
}

// GetProfitStats calculates profit statistics for completed cycles
func (db *DB) GetProfitStats() (avgProfit, totalProfit float64, err error) {
	query := `
		SELECT 
			(so.price - bo.price) * bo.amount - bo.fees - so.fees as profit
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL AND bo.status = 'FILLED' AND so.status = 'FILLED'
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get profit stats: %w", err)
	}
	defer rows.Close()

	profits := []float64{}
	for rows.Next() {
		var profit float64
		if err := rows.Scan(&profit); err != nil {
			continue
		}
		profits = append(profits, profit)
		totalProfit += profit
	}

	if len(profits) > 0 {
		avgProfit = totalProfit / float64(len(profits))
	}

	return avgProfit, totalProfit, nil
}

// CalculateProfitStats calculates profit statistics for completed cycles
func (db *DB) CalculateProfitStats() (avgProfit float64, totalProfit float64) {
	// Requête pour calculer les profits des cycles terminés
	query := `
		SELECT 
			(so.price - bo.price) * bo.amount - bo.fees - coalesce(so.fees, 0) as profit
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		LEFT JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL AND bo.status = 'FILLED' AND so.status = 'FILLED'
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()

	profits := []float64{}
	for rows.Next() {
		var profit float64
		if err := rows.Scan(&profit); err != nil {
			continue
		}
		profits = append(profits, profit)
		totalProfit += profit
	}

	if len(profits) > 0 {
		avgProfit = totalProfit / float64(len(profits))
	}

	return avgProfit, totalProfit
}

// GetRecentActivity retrieves recent activity for the dashboard
func (db *DB) GetRecentActivity(limit int) ([]map[string]interface{}, error) {
	query := `
		SELECT 
			'order' as type,
			id,
			external_id as reference,
			side,
			price,
			amount,
			status,
			created_at,
			updated_at
		FROM orders 
		WHERE updated_at > datetime('now', '-24 hours')
		UNION ALL
		SELECT 
			'cycle' as type,
			id,
			'#' || id as reference,
			CASE WHEN sell_order_id IS NULL THEN 'ACTIVE' ELSE 'COMPLETED' END as side,
			0 as price,
			0 as amount,
			CASE WHEN sell_order_id IS NULL THEN 'ACTIVE' ELSE 'COMPLETED' END as status,
			created_at,
			updated_at
		FROM cycles
		WHERE updated_at > datetime('now', '-24 hours')
		ORDER BY updated_at DESC
		LIMIT ?
	`

	rows, err := db.conn.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent activity: %w", err)
	}
	defer rows.Close()

	var activities []map[string]interface{}
	for rows.Next() {
		var activity = make(map[string]interface{})
		var actType, reference, side, status string
		var id int
		var price, amount float64
		var createdAt, updatedAt time.Time

		err := rows.Scan(&actType, &id, &reference, &side, &price, &amount, &status, &createdAt, &updatedAt)
		if err != nil {
			continue
		}

		activity["type"] = actType
		activity["id"] = id
		activity["reference"] = reference
		activity["side"] = side
		activity["price"] = price
		activity["amount"] = amount
		activity["status"] = status
		activity["created_at"] = createdAt
		activity["updated_at"] = updatedAt

		activities = append(activities, activity)
	}

	return activities, nil
}

// GetDashboardMetrics retrieves all metrics for the dashboard
func (db *DB) GetDashboardMetrics() (map[string]interface{}, error) {
	stats, err := db.GetStats()
	if err != nil {
		return nil, err
	}

	// Ajouter les profits
	avgProfit, totalProfit, err := db.GetProfitStats()
	if err != nil {
		return nil, err
	}

	stats["avg_profit"] = avgProfit
	stats["total_profit"] = totalProfit

	// Calculer le taux de réussite
	filled, _ := stats["filled_orders"].(int)
	cancelled, _ := stats["cancelled_orders"].(int)
	pending, _ := stats["pending_orders"].(int)
	totalOrders := filled + cancelled + pending

	if totalOrders > 0 {
		stats["success_rate"] = (float64(filled) / float64(totalOrders)) * 100
	} else {
		stats["success_rate"] = 0.0
	}

	// Activité récente
	recentActivity, err := db.GetRecentActivity(10)
	if err == nil {
		stats["recent_activity"] = recentActivity
	}

	return stats, nil
}
