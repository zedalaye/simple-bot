package scheduler

import (
	"bot/internal/algorithms"
	"bot/internal/premium"

	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"
	"bot/internal/telegram"
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

type StrategyMarket interface {
	FormatAmount(amount float64) string
	FormatPrice(price float64) string
	GetBaseAsset() string
	GetQuoteAsset() string
	GetPrecision() algorithms.MarketPrecision
}

// StrategyManager orchestrates the execution of trading strategies
type StrategyManager struct {
	exchangeName      string
	pair              string
	db                *database.DB
	market            StrategyMarket
	marketCollector   *market.MarketDataCollector
	calculator        *market.Calculator
	algorithmRegistry *algorithms.AlgorithmRegistry
	exchange          StrategyExchange
}

// NewStrategyManager creates a new strategy manager
func NewStrategyManager(exchangeName, pair string, db *database.DB, market StrategyMarket, marketCollector *market.MarketDataCollector, calculator *market.Calculator, algorithmRegistry *algorithms.AlgorithmRegistry, exchange StrategyExchange) *StrategyManager {
	return &StrategyManager{
		exchangeName:      exchangeName,
		pair:              pair,
		db:                db,
		market:            market,
		marketCollector:   marketCollector,
		calculator:        calculator,
		algorithmRegistry: algorithmRegistry,
		exchange:          exchange,
	}
}

// validateAndPrepareStrategy performs common validation and setup for strategy execution
func (sm *StrategyManager) validateAndPrepareStrategy(strategy database.Strategy) (algorithms.Algorithm,
	algorithms.TradingContext, error) {
	// Premium check
	if err := premium.CheckPremiumness(); err != nil {
		return nil, algorithms.TradingContext{}, fmt.Errorf("premium subscription check failed: %w", err)
	}

	// Get the algorithm for this strategy
	algorithm, exists := sm.algorithmRegistry.Get(strategy.AlgorithmName)
	if !exists {
		return nil, algorithms.TradingContext{}, fmt.Errorf("algorithm %s not found for strategy %s", strategy.
			AlgorithmName, strategy.Name)
	}

	// Validate strategy configuration
	if err := algorithm.ValidateConfig(strategy); err != nil {
		return nil, algorithms.TradingContext{}, fmt.Errorf("invalid strategy configuration: %w", err)
	}

	// Create trading context
	tradingContext := algorithms.TradingContext{
		ExchangeName: sm.exchangeName,
		Pair:         sm.pair,
		CurrentPrice: 0,
		Calculator:   sm.calculator,
		Precision:    sm.market.GetPrecision(),
	}

	return algorithm, tradingContext, nil
}

func (sm *StrategyManager) ExecuteBuyStrategy(strategy database.Strategy) error {
	logger.Infof("[%s] Executing BUY strategy '%s' (%s)", sm.exchangeName, strategy.Name, strategy.AlgorithmName)

	// Common validation and setup
	algorithm, tradingContext, err := sm.validateAndPrepareStrategy(strategy)
	if err != nil {
		return err
	}

	// Check if strategy has reached max concurrent cycles
	if strategy.MaxConcurrentCycles > 0 {
		activeCycles, err := sm.countActiveCyclesForStrategy(strategy.ID)
		if err != nil {
			return fmt.Errorf("failed to count active orders: %w", err)
		}

		if activeCycles >= strategy.MaxConcurrentCycles {
			logger.Infof("[%s] Strategy %s has reached max concurrent cycles (%d/%d), skipping buy execution",
				sm.exchangeName, strategy.Name, activeCycles, strategy.MaxConcurrentCycles)
			return nil
		}
	}

	// Get current market data
	currentPrice, err := sm.exchange.GetPrice(sm.pair)
	if err != nil {
		return fmt.Errorf("failed to get current price: %w", err)
	}
	tradingContext.CurrentPrice = currentPrice

	// Get balance
	balance, err := sm.exchange.FetchBalance()
	if err != nil {
		return fmt.Errorf("failed to fetch balance: %w", err)
	}

	// Check if algorithm wants to buy
	buySignal, err := algorithm.ShouldBuy(tradingContext, strategy)
	if err != nil {
		return fmt.Errorf("algorithm ShouldBuy failed: %w", err)
	}

	if buySignal.ShouldBuy {
		freeQuoteBalance := 0.0
		if quoteBalance, exists := balance[sm.market.GetQuoteAsset()]; exists {
			freeQuoteBalance = quoteBalance.Free
		}
		if freeQuoteBalance < strategy.QuoteAmount {
			logger.Warnf("[%s] Strategy %s: insufficient balance (%.2f < %.2f). Let exchange handle it",
				sm.exchangeName, strategy.Name, freeQuoteBalance, strategy.QuoteAmount)
		}

		// Execute buy order
		err = sm.executeBuyOrder(buySignal, strategy)
		if err != nil {
			return fmt.Errorf("failed to execute buy order: %w", err)
		}

		logger.Infof("[%s] Strategy %s: buy order executed successfully", sm.exchangeName, strategy.Name)
	} else {
		logger.Debugf("[%s] Strategy %s: no buy signal - %s", sm.exchangeName, strategy.Name, buySignal.Reason)
	}

	return nil
}

func (sm *StrategyManager) ExecuteSellStrategy(strategy database.Strategy, currentPrice float64) error {
	// Common validation and setup
	algorithm, tradingContext, err := sm.validateAndPrepareStrategy(strategy)
	if err != nil {
		return err
	}
	tradingContext.CurrentPrice = currentPrice

	logger.Infof("[%s] Executing SELL strategy '%s' (%s)", sm.exchangeName, strategy.Name, strategy.
		AlgorithmName)

	// Check sell signals for open positions
	err = sm.checkSellSignals(algorithm, tradingContext, strategy)
	if err != nil {
		logger.Errorf("Failed to check sell signals for strategy %s: %v", strategy.Name, err)
	}

	return nil
}

// executeBuyOrder executes a buy order from algorithm signal
func (sm *StrategyManager) executeBuyOrder(buySignal algorithms.BuySignal, strategy database.Strategy) error {
	logger.Infof("[%s] Executing buy order for strategy %s: amount=%.4f, price=%.4f",
		sm.exchangeName, strategy.Name, buySignal.Amount, buySignal.LimitPrice)

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
	cycle, err := sm.db.CreateCycle(dbOrder.ID, buySignal.TargetPrice)
	if err != nil {
		logger.Errorf("Failed to create cycle: %v", err)
	}

	// ✅ AJOUTER : Notification Telegram pour ordre d'achat
	message := fmt.Sprintf("🌀 New Cycle on %s [%d]", sm.exchangeName, cycle.ID)
	message += fmt.Sprintf("\n📊 Strategy: %s", strategy.Name)
	message += fmt.Sprintf("\n🛒 Buy Order: %s", *order.Id)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", sm.market.FormatAmount(buySignal.Amount), sm.market.GetBaseAsset())
	message += fmt.Sprintf("\n📉 Buy Price: %s %s", sm.market.FormatPrice(buySignal.LimitPrice), sm.market.GetQuoteAsset())
	message += fmt.Sprintf("\n🎯 Target: %s %s", sm.market.FormatPrice(buySignal.TargetPrice), sm.market.GetQuoteAsset())
	message += fmt.Sprintf("\n💲 Value: %.2f %s", buySignal.Amount*buySignal.LimitPrice, sm.market.GetQuoteAsset())

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send Telegram notification: %v", err)
	}

	logger.Infof("[%s] Buy order created: Order ID=%d, Cycle ID=%d, Strategy=%s",
		sm.exchangeName, dbOrder.ID, cycle.ID, strategy.Name)

	return nil
}

func (sm *StrategyManager) executeSellOrder(sellSignal algorithms.SellSignal, cycle database.CycleEnhanced, strategy database.Strategy) error {
	logger.Infof("[%s] Executing sell order for strategy %s: Cycle=%d, Amount=%.4f, Price=%.4f",
		sm.exchangeName, strategy.Name, cycle.ID, cycle.BuyOrder.Amount, sellSignal.LimitPrice)

	// Place order on exchange
	order, err := sm.exchange.PlaceLimitSellOrder(sm.pair, cycle.BuyOrder.Amount, sellSignal.LimitPrice)
	if err != nil {
		return fmt.Errorf("failed to place sell order on exchange: %w", err)
	}

	// Save sell order to database with strategy ID
	dbSellOrder, err := sm.db.CreateOrder(*order.Id, database.Sell, *order.Amount, *order.Price, 0.0, strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to save sell order to database: %w", err)
	}

	// Associate sell order with cycle
	err = sm.db.UpdateCycleSellOrder(cycle.ID, dbSellOrder.ID)
	if err != nil {
		logger.Errorf("Failed to associate sell order with cycle: %v", err)
		return fmt.Errorf("failed to associate sell order with cycle: %w", err)
	}

	expectedProfit := (sellSignal.LimitPrice-cycle.BuyOrder.Price)*cycle.BuyOrder.Amount - cycle.BuyOrder.Fees

	message := fmt.Sprintf("🌀 Cycle on %s [%d] UPDATE", sm.exchangeName, cycle.ID)
	message += fmt.Sprintf("\n📊 Strategy: %s", strategy.Name)
	message += fmt.Sprintf("\n🚀 Sell Order: %s", *order.Id)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", sm.market.FormatAmount(cycle.BuyOrder.Amount), sm.market.GetBaseAsset())
	message += fmt.Sprintf("\n📈 Sell Price: %s %s", sm.market.FormatPrice(sellSignal.LimitPrice), sm.market.GetQuoteAsset())
	message += fmt.Sprintf("\n💲 Value: %.2f %s", cycle.BuyOrder.Amount*sellSignal.LimitPrice, sm.market.GetQuoteAsset())
	message += fmt.Sprintf("\n🤑 Expected Profit: %.2f %s (%.2f%%)", expectedProfit, sm.market.GetQuoteAsset(),
		(expectedProfit/(cycle.BuyOrder.Price*cycle.BuyOrder.Amount))*100)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send Telegram notification: %v", err)
	}

	logger.Infof("[%s] Sell order created: Order ID=%d, Cycle ID=%d, Strategy=%s, Expected profit=%.2f",
		sm.exchangeName,
		dbSellOrder.ID, cycle.ID, strategy.Name,
		expectedProfit)

	return nil
}

// checkSellSignals checks if any positions should be sold
func (sm *StrategyManager) checkSellSignals(algorithm algorithms.Algorithm, ctx algorithms.TradingContext, strategy database.Strategy) error {
	openCycles, err := sm.getOpenCyclesForStrategy(strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to get open cycles for strategy %d: %w", strategy.ID, err)
	}

	for _, cycle := range openCycles {
		sellSignal, err := algorithm.ShouldSell(ctx, cycle.Cycle, strategy)
		if err != nil {
			logger.Errorf("Algorithm ShouldSell failed for cycle %d: %v", cycle.ID, err)
			continue
		}

		if sellSignal.ShouldSell {
			logger.Infof("[%s] Strategy %s: sell signal for cycle %d - %s", sm.exchangeName, strategy.Name, cycle.ID, sellSignal.Reason)

			err = sm.executeSellOrder(sellSignal, cycle, strategy)
			if err != nil {
				logger.Errorf("Failed to execute sell order for cycle %d: %v", cycle.ID, err)
				continue
			}

			logger.Infof("[%s] Strategy %s: sell order executed successfully for cycle %d", sm.exchangeName, strategy.Name, cycle.ID)
		}
	}

	return nil
}

// Helper methods
func (sm *StrategyManager) countActiveCyclesForStrategy(strategyID int) (int, error) {
	return sm.db.CountActiveCyclesForStrategy(strategyID)
}

func (sm *StrategyManager) getOpenCyclesForStrategy(strategyID int) ([]database.CycleEnhanced, error) {
	return sm.db.GetOpenCyclesForStrategy(strategyID)
}
