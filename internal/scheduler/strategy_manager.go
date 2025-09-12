package scheduler

import (
	"bot/internal/algorithms"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"
	"fmt"
)

// StrategyExchange defines the interface needed for strategy execution
type StrategyExchange interface {
	FetchBalance() (map[string]ExchangeBalance, error)
	GetPrice(pair string) (float64, error)
	PlaceLimitBuyOrder(pair string, amount float64, price float64) (ExchangeOrder, error)
	PlaceLimitSellOrder(pair string, amount float64, price float64) (ExchangeOrder, error)
}

// ExchangeBalance represents balance from exchange
type ExchangeBalance struct {
	Free float64
}

// ExchangeOrder represents an order from exchange
type ExchangeOrder struct {
	Id     *string
	Price  *float64
	Amount *float64
	Status *string
}

// StrategyManager orchestrates the execution of trading strategies
type StrategyManager struct {
	pair              string
	db                *database.DB
	marketCollector   *market.MarketDataCollector
	calculator        *market.Calculator
	algorithmRegistry *algorithms.AlgorithmRegistry
	exchange          StrategyExchange
	resourceManager   *ResourceManager
}

// NewStrategyManager creates a new strategy manager
func NewStrategyManager(pair string, db *database.DB, marketCollector *market.MarketDataCollector, calculator *market.Calculator, algorithmRegistry *algorithms.AlgorithmRegistry, exchange StrategyExchange) *StrategyManager {
	return &StrategyManager{
		pair:              pair,
		db:                db,
		marketCollector:   marketCollector,
		calculator:        calculator,
		algorithmRegistry: algorithmRegistry,
		exchange:          exchange,
		resourceManager:   NewResourceManager(exchange),
	}
}

// ExecuteStrategy executes a single strategy
func (sm *StrategyManager) ExecuteStrategy(strategy database.Strategy) error {
	logger.Infof("🎯 Executing strategy '%s' (algorithm: %s)", strategy.Name, strategy.AlgorithmName)

	// Get the algorithm for this strategy
	algorithm, exists := sm.algorithmRegistry.Get(strategy.AlgorithmName)
	if !exists {
		return fmt.Errorf("algorithm %s not found for strategy %s", strategy.AlgorithmName, strategy.Name)
	}

	// Validate strategy configuration
	if err := algorithm.ValidateConfig(strategy); err != nil {
		return fmt.Errorf("invalid strategy configuration: %w", err)
	}

	// Check if strategy has reached max concurrent orders
	activeOrders, err := sm.countActiveOrdersForStrategy(strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to count active orders: %w", err)
	}

	if activeOrders >= strategy.MaxConcurrentOrders {
		logger.Infof("Strategy %s has reached max concurrent orders (%d/%d), skipping execution",
			strategy.Name, activeOrders, strategy.MaxConcurrentOrders)
		return nil
	}

	// Get current market data
	currentPrice, err := sm.exchange.GetPrice(sm.pair)
	if err != nil {
		return fmt.Errorf("failed to get current price: %w", err)
	}

	// Get balance
	exchangeBalance, err := sm.exchange.FetchBalance()
	if err != nil {
		return fmt.Errorf("failed to fetch balance: %w", err)
	}

	// Convert exchange balance to algorithm balance
	balance := make(map[string]algorithms.Balance)
	for asset, bal := range exchangeBalance {
		balance[asset] = algorithms.Balance{Free: bal.Free}
	}

	// Get open cycles for this strategy
	openCycles, err := sm.getOpenCyclesForStrategy(strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to get open cycles for strategy %v: %w", strategy.ID, err)
	}

	// Create trading context
	tradingContext := algorithms.TradingContext{
		Pair:         sm.pair,
		CurrentPrice: currentPrice,
		Balance:      balance,
		OpenCycles:   openCycles,
		Calculator:   sm.calculator,
	}

	// Check if algorithm wants to buy
	buySignal, err := algorithm.ShouldBuy(tradingContext, strategy)
	if err != nil {
		return fmt.Errorf("algorithm ShouldBuy failed: %w", err)
	}

	if buySignal.ShouldBuy {
		// Try to reserve balance for this purchase
		reserved, err := sm.resourceManager.ReserveBalance(strategy.QuoteAmount)
		if err != nil {
			return fmt.Errorf("failed to check balance availability: %w", err)
		}

		if !reserved {
			logger.Warnf("Strategy %s: insufficient balance for quote_amount %.2f, skipping buy",
				strategy.Name, strategy.QuoteAmount)
			return nil
		}

		// Execute buy order
		err = sm.executeBuyOrder(buySignal, strategy)
		if err != nil {
			// Release balance on failure
			sm.resourceManager.ReleaseBalance(strategy.QuoteAmount)
			return fmt.Errorf("failed to execute buy order: %w", err)
		}

		logger.Infof("✅ Strategy %s: buy order executed successfully", strategy.Name)
	} else {
		logger.Debugf("Strategy %s: no buy signal - %s", strategy.Name, buySignal.Reason)
	}

	// Check sell signals for open positions
	err = sm.checkSellSignals(algorithm, tradingContext, strategy, openCycles)
	if err != nil {
		logger.Errorf("Failed to check sell signals for strategy %s: %v", strategy.Name, err)
	}

	return nil
}

// executeBuyOrder executes a buy order from algorithm signal
func (sm *StrategyManager) executeBuyOrder(buySignal algorithms.BuySignal, strategy database.Strategy) error {
	logger.Infof("Executing buy order for strategy %s: amount=%.4f, price=%.4f",
		strategy.Name, buySignal.Amount, buySignal.LimitPrice)

	// Place order on exchange
	order, err := sm.exchange.PlaceLimitBuyOrder(sm.pair, buySignal.Amount, buySignal.LimitPrice)
	if err != nil {
		return fmt.Errorf("failed to place buy order on exchange: %w", err)
	}

	// Save order to database with strategy ID
	dbOrder, err := sm.db.CreateOrder(*order.Id, database.Buy, *order.Amount, *order.Price, 0.0, strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to save buy order to database: %w", err)
	}

	// Create cycle with strategy ID
	cycle, err := sm.db.CreateCycle(dbOrder.ID)
	if err != nil {
		logger.Errorf("Failed to create cycle: %v", err)
	}

	logger.Infof("✅ Buy order created: Order ID=%d, Cycle ID=%d, Strategy=%s",
		dbOrder.ID, cycle.ID, strategy.Name)

	return nil
}

// checkSellSignals checks if any positions should be sold
func (sm *StrategyManager) checkSellSignals(algorithm algorithms.Algorithm, ctx algorithms.TradingContext, strategy database.Strategy, cycles []database.CycleEnhanced) error {
	for _, cycle := range cycles {
		sellSignal, err := algorithm.ShouldSell(ctx, cycle.Cycle, strategy)
		if err != nil {
			logger.Errorf("Algorithm ShouldSell failed for cycle %d: %v", cycle.ID, err)
			continue
		}

		if sellSignal.ShouldSell {
			logger.Infof("Strategy %s: sell signal for cycle %d - %s", strategy.Name, cycle.ID, sellSignal.Reason)
			// TODO: Implement sell order execution
		}
	}

	return nil
}

// Helper methods
func (sm *StrategyManager) countActiveOrdersForStrategy(strategyID int) (int, error) {
	return sm.db.CountActiveOrdersForStrategy(strategyID)
}

func (sm *StrategyManager) getOpenCyclesForStrategy(strategyID int) ([]database.CycleEnhanced, error) {
	return sm.db.GetOpenCyclesForStrategy(strategyID)
}
