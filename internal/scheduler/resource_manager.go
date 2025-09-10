package scheduler

import (
	"bot/internal/logger"
	"sync"
)

// ResourceManager manages shared resources (balance) between strategies
type ResourceManager struct {
	exchange       StrategyExchange
	reservedAmount float64
	mutex          sync.Mutex
}

// NewResourceManager creates a new resource manager
func NewResourceManager(exchange StrategyExchange) *ResourceManager {
	return &ResourceManager{
		exchange:       exchange,
		reservedAmount: 0.0,
		mutex:          sync.Mutex{},
	}
}

// ReserveBalance tries to reserve the specified amount for a strategy
func (rm *ResourceManager) ReserveBalance(amount float64) (bool, error) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Get current balance
	balance, err := rm.exchange.FetchBalance()
	if err != nil {
		return false, err
	}

	// Check USDC balance (assuming USDC is the quote currency)
	usdcBalance, exists := balance["USDC"]
	if !exists {
		logger.Warn("USDC balance not found")
		return false, nil
	}

	// Calculate available balance (free - already reserved)
	availableBalance := usdcBalance.Free - rm.reservedAmount

	if availableBalance < amount {
		logger.Debugf("Insufficient balance: need %.2f, available %.2f (free: %.2f, reserved: %.2f)",
			amount, availableBalance, usdcBalance.Free, rm.reservedAmount)
		return false, nil
	}

	// Reserve the amount
	rm.reservedAmount += amount
	logger.Debugf("Reserved %.2f USDC, total reserved: %.2f", amount, rm.reservedAmount)

	return true, nil
}

// ReleaseBalance releases a reserved amount back to the pool
func (rm *ResourceManager) ReleaseBalance(amount float64) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if rm.reservedAmount >= amount {
		rm.reservedAmount -= amount
		logger.Debugf("Released %.2f USDC, total reserved: %.2f", amount, rm.reservedAmount)
	} else {
		logger.Warnf("Trying to release %.2f but only %.2f reserved", amount, rm.reservedAmount)
		rm.reservedAmount = 0
	}

	return nil
}

// GetAvailableBalance returns the currently available balance
func (rm *ResourceManager) GetAvailableBalance() (float64, error) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Get current balance
	balance, err := rm.exchange.FetchBalance()
	if err != nil {
		return 0, err
	}

	// Check USDC balance
	usdcBalance, exists := balance["USDC"]
	if !exists {
		return 0, nil
	}

	// Return available balance (free - reserved)
	availableBalance := usdcBalance.Free - rm.reservedAmount
	return availableBalance, nil
}

// GetStats returns resource manager statistics
func (rm *ResourceManager) GetStats() map[string]interface{} {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	return map[string]interface{}{
		"reserved_amount": rm.reservedAmount,
		"status":          "active",
	}
}
