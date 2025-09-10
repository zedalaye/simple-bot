package bot

import (
	"bot/internal/algorithms"
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"
	"bot/internal/telegram"
	"fmt"
	"math"
	"strconv"
	"time"
)

type Exchange interface {
	GetMarket(pair string) Market
	GetMarketsList() []Market
	FetchBalance() (map[string]Balance, error)
	PlaceLimitBuyOrder(pair string, amount float64, price float64) (Order, error)
	PlaceLimitSellOrder(pair string, amount float64, price float64) (Order, error)
	FetchOrder(id string, symbol string) (Order, error)
	CancelOrder(id string, symbol string) (Order, error)
	GetPrice(pair string) (float64, error)
	FetchCandles(pair string, timeframe string, since *int64, limit int64) ([]Candle, error)
	FetchMyTrades(pair string, since *int64, until *int64, limit int64) ([]Trade, error)
	FetchTradesForOrder(id string, pair string) ([]Trade, error)
}

type Market struct {
	Symbol     string
	BaseAsset  string
	BaseId     string
	QuoteAsset string
	QuoteId    string
	Precision  struct {
		Price          float64
		PriceDecimals  int
		Amount         float64
		AmountDecimals int
	}
}

type Balance struct {
	Free float64
}

type Order struct {
	Id        *string
	Price     *float64
	Amount    *float64
	Status    *string
	Timestamp *int64
}

type Trade struct {
	Id           *string
	Timestamp    *int64
	Symbol       *string
	OrderId      *string
	Type         *string
	Side         *string
	TakerOrMaker *string
	Price        *float64
	Amount       *float64
	Cost         *float64
	Fee          *float64
	FeeToken     *string
}

type Candle struct {
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

type Bot struct {
	Config            config.BotConfig
	db                *database.DB
	exchange          Exchange
	market            Market
	marketCollector   *market.MarketDataCollector
	calculator        *market.Calculator
	algorithmRegistry *algorithms.AlgorithmRegistry
	strategyMode      bool // true = use strategies from DB, false = use legacy config
	done              chan bool
}

func NewBot(config config.BotConfig, db *database.DB, exchange Exchange) (*Bot, error) {
	bot := &Bot{
		Config:   config,
		db:       db,
		exchange: exchange,
		done:     make(chan bool),
	}

	if err := bot.initializeMarketPrecision(); err != nil {
		return nil, err
	}

	// Initialize market data services with adapter
	exchangeAdapter := newBotExchangeAdapter(exchange)
	bot.marketCollector = market.NewMarketDataCollector(db, exchangeAdapter)
	bot.calculator = market.NewCalculator(db, bot.marketCollector)

	// Initialize algorithm registry
	bot.algorithmRegistry = algorithms.NewAlgorithmRegistry()
	logger.Infof("[%s] Algorithm registry initialized with %d algorithms", config.ExchangeName, len(bot.algorithmRegistry.List()))

	// Check if we should use strategy mode (multiple strategies available)
	strategies, err := db.GetEnabledStrategies()
	if err == nil && len(strategies) > 1 {
		bot.strategyMode = true
		logger.Infof("[%s] Strategy mode enabled: found %d strategies in database", config.ExchangeName, len(strategies))
	} else {
		bot.strategyMode = false
		logger.Infof("[%s] Legacy mode: using single strategy from config", config.ExchangeName)
	}

	// Initial market data collection
	logger.Infof("[%s] Initializing market data collection...", config.ExchangeName)
	err = bot.marketCollector.CollectCandles(config.Pair, "4h", 200) // Collect initial historical data
	if err != nil {
		logger.Warnf("Failed to collect initial market data: %v", err)
	} else {
		logger.Infof("[%s] Market data collection initialized successfully", config.ExchangeName)
	}

	return bot, nil
}

func (b *Bot) Cleanup() {
	if b.db != nil {
		logger.Infof("[%s] Close database connection...", b.Config.ExchangeName)
		_ = b.db.Close()
		b.db = nil
	}
}

func (b *Bot) initializeMarketPrecision() error {
	logger.Infof("[%s] Fetching market data...", b.Config.ExchangeName)
	b.market = b.exchange.GetMarket(b.Config.Pair)
	logger.Infof("[%s] Base Asset: %s, Quote Asset: %s", b.Config.ExchangeName, b.market.BaseAsset, b.market.QuoteAsset)
	logger.Infof("[%s] Market precision: price=%f, amount=%f", b.Config.ExchangeName, b.market.Precision.Price, b.market.Precision.Amount)
	return nil
}

func (b *Bot) Start(buyAtLaunch bool) error {
	logger.Infof("[%s] Starting bot...", b.Config.ExchangeName)

	b.handleOrderCheck()
	b.handlePriceCheck()
	b.ShowStatistics()

	if buyAtLaunch {
		b.handleBuySignal()
	}

	logger.Debug("Starting bot goroutine...")
	go b.run()
	return nil
}

func (b *Bot) Stop() {
	logger.Infof("[%s] Stopping bot...", b.Config.ExchangeName)
	close(b.done)
}

func (b *Bot) run() {
	if b.strategyMode {
		b.runStrategyMode()
	} else {
		b.runLegacyMode()
	}
}

func (b *Bot) runStrategyMode() {
	logger.Info("[Strategy Mode] Running with database-configured strategies...")

	// In strategy mode, we execute strategies based on their individual schedules
	// For now, let's run a simple demo that executes strategies periodically
	checkTicker := time.NewTicker(b.Config.CheckInterval)
	defer checkTicker.Stop()

	// Demo: Execute strategies every 30 seconds for testing
	strategyTicker := time.NewTicker(30 * time.Second)
	defer strategyTicker.Stop()

	for {
		select {
		case <-b.done:
			logger.Infof("[%s] Bot stopped gracefully (strategy mode)", b.Config.ExchangeName)
			return
		case <-strategyTicker.C:
			b.executeAllStrategiesDemo()
		case <-checkTicker.C:
			b.handlePriceCheck()
			b.handleOrderCheck()
			b.ShowStatistics()
		}
	}
}

func (b *Bot) runLegacyMode() {
	logger.Info("[Legacy Mode] Running with traditional single strategy...")

	buyTicker := time.NewTicker(b.Config.BuyInterval)
	defer buyTicker.Stop()

	checkTicker := time.NewTicker(b.Config.CheckInterval)
	defer checkTicker.Stop()

	for {
		select {
		case <-b.done:
			logger.Infof("[%s] Bot stopped gracefully (legacy mode)", b.Config.ExchangeName)
			return
		case <-buyTicker.C:
			b.handleBuySignal()
		case <-checkTicker.C:
			b.handlePriceCheck()
			b.handleOrderCheck()
			b.ShowStatistics()
		}
	}
}

func (b *Bot) executeAllStrategiesDemo() {
	logger.Debug("🎯 Checking all strategies for execution...")

	strategies, err := b.db.GetEnabledStrategies()
	if err != nil {
		logger.Errorf("Failed to get enabled strategies: %v", err)
		return
	}

	for _, strategy := range strategies {
		err := b.executeStrategyDemo(strategy)
		if err != nil {
			logger.Errorf("Failed to execute strategy %s: %v", strategy.Name, err)
		}
	}
}

func (b *Bot) executeStrategyDemo(strategy database.Strategy) error {
	logger.Infof("🎯 Demo execution of strategy '%s' (algorithm: %s)", strategy.Name, strategy.AlgorithmName)

	// Get the algorithm for this strategy
	algorithm, exists := b.algorithmRegistry.Get(strategy.AlgorithmName)
	if !exists {
		return fmt.Errorf("algorithm %s not found", strategy.AlgorithmName)
	}

	// Check active orders limit
	activeOrders, err := b.db.CountActiveOrdersForStrategy(strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to count active orders: %w", err)
	}

	if activeOrders >= strategy.MaxConcurrentOrders {
		logger.Infof("Strategy %s has reached max concurrent orders (%d/%d), skipping",
			strategy.Name, activeOrders, strategy.MaxConcurrentOrders)
		return nil
	}

	// Get current market data
	currentPrice, err := b.exchange.GetPrice(b.Config.Pair)
	if err != nil {
		return fmt.Errorf("failed to get current price: %w", err)
	}

	// Get balance
	exchangeBalance, err := b.exchange.FetchBalance()
	if err != nil {
		return fmt.Errorf("failed to fetch balance: %w", err)
	}

	// Convert balance format
	balance := make(map[string]algorithms.Balance)
	for asset, bal := range exchangeBalance {
		balance[asset] = algorithms.Balance{Free: bal.Free}
	}

	// Get open positions for this strategy
	openPositions, err := b.db.GetOpenPositionsForStrategy(strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to get open positions: %w", err)
	}

	// Create trading context
	tradingContext := algorithms.TradingContext{
		Pair:          b.Config.Pair,
		CurrentPrice:  currentPrice,
		Balance:       balance,
		OpenPositions: openPositions,
		Calculator:    b.calculator,
	}

	// Ask algorithm if we should buy
	buySignal, err := algorithm.ShouldBuy(tradingContext, strategy)
	if err != nil {
		return fmt.Errorf("algorithm ShouldBuy failed: %w", err)
	}

	if buySignal.ShouldBuy {
		logger.Infof("✅ Strategy %s: BUY signal - %s", strategy.Name, buySignal.Reason)
		// For demo, just log the signal without actually placing orders
		logger.Infof("   Would buy %.4f at %.4f with target %.4f",
			buySignal.Amount, buySignal.LimitPrice, buySignal.TargetPrice)
	} else {
		logger.Debugf("Strategy %s: no buy signal - %s", strategy.Name, buySignal.Reason)
	}

	return nil
}

func (b *Bot) handleBuySignal() {
	// Use algorithm-based approach with Legacy strategy
	err := b.handleBuySignalWithAlgorithm()
	if err != nil {
		logger.Errorf("Algorithm-based buy signal failed, falling back to legacy: %v", err)
		// Fallback to legacy logic for now
		b.handleBuySignalLegacy()
	}
}

func (b *Bot) handleBuySignalWithAlgorithm() error {
	// Get the Legacy strategy (ID=1) to maintain backward compatibility
	strategy, err := b.db.GetStrategy(1)
	if err != nil {
		return fmt.Errorf("failed to get legacy strategy: %w", err)
	}

	// Get the algorithm for this strategy
	algorithm, exists := b.algorithmRegistry.Get(strategy.AlgorithmName)
	if !exists {
		return fmt.Errorf("algorithm %s not found", strategy.AlgorithmName)
	}

	// Check daily buy limit
	todayBuyCount, err := b.db.CountTodayBuyOrders()
	if err != nil {
		return fmt.Errorf("failed to count today's buy orders: %w", err)
	}

	if todayBuyCount >= b.Config.MaxBuysPerDay {
		logger.Infof("[%s] Daily buy limit reached (%d/%d), skipping buy signal",
			b.Config.ExchangeName, todayBuyCount, b.Config.MaxBuysPerDay)
		return nil
	}

	// Check available balance
	exchangeBalance, err := b.exchange.FetchBalance()
	if err != nil {
		return fmt.Errorf("failed to fetch balances: %w", err)
	}

	quoteAssetBalance, ok := exchangeBalance[b.market.QuoteAsset]
	if !ok || (quoteAssetBalance.Free < strategy.QuoteAmount) {
		logger.Warnf("[%s] %s balance not found or insufficient: %v",
			b.Config.ExchangeName, b.market.QuoteAsset, quoteAssetBalance.Free)
		return nil
	}

	// Get current price
	currentPrice, err := b.exchange.GetPrice(b.Config.Pair)
	if err != nil {
		return fmt.Errorf("failed to get current price: %w", err)
	}

	// Create trading context
	balance := make(map[string]algorithms.Balance)
	for asset, bal := range exchangeBalance {
		balance[asset] = algorithms.Balance{Free: bal.Free}
	}

	openPositions, err := b.db.GetOpenPositions()
	if err != nil {
		return fmt.Errorf("failed to get open positions: %w", err)
	}

	tradingContext := algorithms.TradingContext{
		Pair:          b.Config.Pair,
		CurrentPrice:  currentPrice,
		Balance:       balance,
		OpenPositions: openPositions,
		Calculator:    b.calculator,
	}

	// Ask algorithm if we should buy
	buySignal, err := algorithm.ShouldBuy(tradingContext, *strategy)
	if err != nil {
		return fmt.Errorf("algorithm ShouldBuy failed: %w", err)
	}

	if !buySignal.ShouldBuy {
		logger.Infof("[%s] Algorithm decision: %s", b.Config.ExchangeName, buySignal.Reason)
		return nil
	}

	// Algorithm says buy, execute the order
	logger.Infof("[%s] Algorithm decision: %s", b.Config.ExchangeName, buySignal.Reason)
	return b.executeBuyOrder(buySignal, *strategy)
}

func (b *Bot) executeBuyOrder(buySignal algorithms.BuySignal, strategy database.Strategy) error {
	// Round prices and amounts according to market precision
	limitPrice := b.roundToPrecision(buySignal.LimitPrice, b.market.Precision.Price)
	baseAmount := b.roundToPrecision(buySignal.Amount, b.market.Precision.Amount)
	targetPrice := b.roundToPrecision(buySignal.TargetPrice, b.market.Precision.Price)

	logger.Infof("[%s] Placing buy order: amount=%s, price=%s, target=%s",
		b.Config.ExchangeName, b.market.FormatAmount(baseAmount),
		b.market.FormatPrice(limitPrice), b.market.FormatPrice(targetPrice))

	// Place the buy order on exchange
	order, err := b.exchange.PlaceLimitBuyOrder(b.Config.Pair, baseAmount, limitPrice)
	if err != nil {
		return fmt.Errorf("failed to place limit buy order: %w", err)
	}

	orderPrice := *order.Price
	orderAmount := *order.Amount

	// Save order to database with strategy ID
	dbOrder, err := b.db.CreateOrderWithStrategy(*order.Id, database.Buy, orderAmount, orderPrice, 0.0, nil, strategy.ID)
	if err != nil {
		return fmt.Errorf("failed to save buy order to database: %w", err)
	}

	// Create cycle with strategy ID
	dbCycle, err := b.db.CreateCycleWithStrategy(dbOrder.ID, strategy.ID)
	if err != nil {
		logger.Errorf("Failed to create cycle in database: %v", err)
	}

	// Create position with pre-calculated target price
	position, err := b.db.CreatePositionWithStrategy(orderPrice, targetPrice, orderAmount, strategy.ID)
	if err != nil {
		logger.Errorf("Failed to create position in database: %v", err)
	} else {
		err = b.db.UpdateOrderPosition(dbOrder.ID, position.ID)
		if err != nil {
			logger.Errorf("Failed to update order position: %v", err)
		}
		logger.Infof("[%s] Position created with PRE-CALCULATED target: ID=%d, target=%s",
			b.Config.ExchangeName, position.ID, b.market.FormatPrice(targetPrice))
	}

	// Send notification
	message := fmt.Sprintf("🌀 New Cycle [Strategy: %s] on %s [%d]", strategy.Name, b.Config.ExchangeName, dbCycle.ID)
	message += fmt.Sprintf("\nℹ️ Buy Order %s [%d]", *order.Id, dbOrder.ID)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", b.market.FormatAmount(orderAmount), b.market.BaseAsset)
	message += fmt.Sprintf("\n📉 Buy Price: %s %s", b.market.FormatPrice(orderPrice), b.market.QuoteAsset)
	message += fmt.Sprintf("\n🎯 Target Price: %s %s (PRE-CALCULATED)", b.market.FormatPrice(targetPrice), b.market.QuoteAsset)
	message += fmt.Sprintf("\n📊 Reason: %s", buySignal.Reason)
	message += fmt.Sprintf("\n💲 Value: %.2f %s", orderAmount*orderPrice, b.market.QuoteAsset)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification: %v", err)
	}

	logger.Infof("[%s] Algorithm-driven buy order placed: %s %s at %s %s (target=%s, strategy=%s)",
		b.Config.ExchangeName, b.market.FormatAmount(orderAmount), b.market.BaseAsset,
		b.market.FormatPrice(orderPrice), b.market.QuoteAsset, b.market.FormatPrice(targetPrice), strategy.Name)

	return nil
}

func (b *Bot) handleBuySignalLegacy() {
	// Original legacy logic (unchanged for fallback)
	logger.Info("Using legacy buy signal logic")

	// Check daily buy limit
	todayBuyCount, err := b.db.CountTodayBuyOrders()
	if err != nil {
		logger.Errorf("Failed to count today's buy orders: %v", err)
		return
	}

	if todayBuyCount >= b.Config.MaxBuysPerDay {
		logger.Infof("[%s] Daily buy limit reached (%d/%d), skipping RSI check",
			b.Config.ExchangeName, todayBuyCount, b.Config.MaxBuysPerDay)
		return
	}

	// Check available balance
	balance, err := b.exchange.FetchBalance()
	if err != nil {
		logger.Errorf("Failed to fetch balances: %v", err)
		return
	}

	quoteAssetBalance, ok := balance[b.market.QuoteAsset]
	if !ok || (quoteAssetBalance.Free < b.Config.QuoteAmount) {
		logger.Warnf("[%s] %s balance not found or insufficient: %v",
			b.Config.ExchangeName, b.market.QuoteAsset, quoteAssetBalance.Free)
		return
	}

	// Vérifier le RSI pour confirmer le signal d'achat
	logger.Infof("[%s] Checking RSI for potential buy signal...", b.Config.ExchangeName)

	rsi, err := b.calculator.CalculateRSI(b.Config.Pair, "4h", b.Config.RSIPeriod)
	if err != nil {
		logger.Errorf("Failed to calculate RSI: %v", err)
		return
	}

	logger.Infof("[%s] Current RSI: %.2f", b.Config.ExchangeName, rsi)

	if b.Config.RSIThreshold > 0 && b.Config.RSIThreshold < 100 {
		if rsi > b.Config.RSIThreshold {
			logger.Infof("[%s] RSI (%.2f) is too high (threshold: %.2f), skipping buy signal",
				b.Config.ExchangeName, rsi, b.Config.RSIThreshold)
			return
		} else {
			logger.Infof("[%s] RSI (%.2f) is below threshold (%.2f), proceeding with buy signal",
				b.Config.ExchangeName,
				rsi, b.Config.RSIThreshold)
		}
	} else {
		logger.Debug("RSI Threshold is not set. Skipping RSI check")
	}

	logger.Infof("[%s] Time to place a new Buy Order...", b.Config.ExchangeName)

	currentPrice, err := b.exchange.GetPrice(b.Config.Pair)
	if err != nil {
		logger.Errorf("Failed to get ticker data: %v", err)
		return
	}

	// Calculate dynamic price offset based on RSI: -(0.1% + (RSI/100)%)
	dynamicOffsetPercent := -((0.1 / 100.0) + (rsi/100.0)/100.0)
	dynamicOffset := currentPrice * dynamicOffsetPercent
	limitPrice := b.roundToPrecision(currentPrice+dynamicOffset, b.market.Precision.Price)
	baseAmount := b.roundToPrecision(b.Config.QuoteAmount/limitPrice, b.market.Precision.Amount)

	logger.Infof("[%s] Dynamic Offset: %.4f (RSI=%.2f, Offset=%.2f%%), Limit Price=%s",
		b.Config.ExchangeName, dynamicOffset, rsi, dynamicOffsetPercent*100, b.market.FormatPrice(limitPrice))

	order, err := b.exchange.PlaceLimitBuyOrder(b.Config.Pair, baseAmount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place Limit Buy Order: %v", err)
		return
	}

	orderPrice := *order.Price
	orderAmount := *order.Amount

	dbOrder, err := b.db.CreateOrder(*order.Id, database.Buy, orderAmount, orderPrice, 0.0, nil)
	if err != nil {
		logger.Errorf("Failed to save buy order to database: %v", err)
		return
	}

	dbCycle, err := b.db.CreateCycle(dbOrder.ID)
	if err != nil {
		logger.Errorf("Failed to create cycle to database: %v", err)
	}

	message := ""
	message += fmt.Sprintf("🌀 New Cycle on %s [%d]", b.Config.ExchangeName, dbCycle.ID)
	message += fmt.Sprintf("\nℹ️ Buy Order %s [%d]", *order.Id, dbOrder.ID)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", b.market.FormatAmount(orderAmount), b.market.BaseAsset)
	message += fmt.Sprintf("\n📉 Buy Price: %s %s", b.market.FormatPrice(orderPrice), b.market.QuoteAsset)
	message += fmt.Sprintf("\n📊 RSI: %.2f", rsi)
	message += fmt.Sprintf("\n💲 Value: %.2f %s", orderAmount*orderPrice, b.market.QuoteAsset)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification to Telegram: %v", err)
	}

	logger.Infof("[%s] Limit Buy Order placed: %s %s at %s %s (ID=%v, DB_ID=%v, RSI=%.2f)",
		b.Config.ExchangeName,
		b.market.FormatAmount(orderAmount), b.market.BaseAsset, b.market.FormatPrice(orderPrice), b.market.QuoteAsset,
		order.Id, dbOrder.ID, rsi)
}

func (b *Bot) handlePriceCheck() {
	logger.Debug("Checking prices...")

	currentPrice, err := b.exchange.GetPrice(b.Config.Pair)
	if err != nil {
		logger.Errorf("Failed to get ticker data: %v", err)
		return
	}

	currentPrice = b.roundToPrecision(currentPrice, b.market.Precision.Price)
	logger.Infof("[%s] Current price: %s", b.Config.ExchangeName, b.market.FormatPrice(currentPrice))

	// Calculer la volatilité pour ajuster le seuil de vente (avec cache)
	volatility, err := b.calculator.CalculateVolatility(b.Config.Pair, "4h", b.Config.VolatilityPeriod)
	if err != nil {
		logger.Errorf("Failed to calculate volatility: %v", err)
		// Utiliser une valeur par défaut en cas d'erreur
		volatility = 2.0 // 2% par défaut
		logger.Infof("[%s] Using default (%.2f%%) volatility !", b.Config.ExchangeName, volatility)
	} else {
		logger.Infof("[%s] Current volatility: %.2f%%", b.Config.ExchangeName, volatility)
	}

	positions, err := b.db.GetOpenPositions()
	if err != nil {
		logger.Errorf("Failed to get open positions from database: %v", err)
		return
	}
	logger.Debugf("Found %d open positions", len(positions))

	for _, pos := range positions {
		// Mettre à jour le prix maximum observé
		if currentPrice > pos.MaxPrice {
			err := b.db.UpdatePositionMaxPrice(pos.ID, currentPrice)
			if err != nil {
				logger.Errorf("Failed to update max price for position %d: %v", pos.ID, err)
				continue
			}
			pos.MaxPrice = currentPrice
			logger.Infof("[%s] Position %d, updated MaxPrice → %s",
				b.Config.ExchangeName, pos.ID, b.market.FormatPrice(pos.MaxPrice))
		}

		// Calculer le seuil de profit dynamique basé sur la volatilité
		// ProfitThreshold est maintenant un pourcentage (2.0 = 2%)
		// Pendant une faible volatilité, accepter des profits plus faibles (plus agressif)
		// Pendant une forte volatilité, espérer des profits plus élevés (moins agressif)

		// Calculer l'ajustement basé sur la distance entre la volatilité et le seuil de base
		// Peut-être négatif si la volatilité est inférieure au seuil de base
		volatilityFactor := (volatility - b.Config.ProfitTarget) / 100.0 // Convertir en décimal (4.0 -> 0.04)
		adjustmentPercent := volatilityFactor * (b.Config.VolatilityAdjustment / 100.0)

		// Appliquer l'ajustement selon le niveau de volatilité
		dynamicProfitPercent := (b.Config.ProfitTarget / 100.0) + adjustmentPercent

		// S'assurer que le seuil reste raisonnable (entre 0.1% et 15%)
		if dynamicProfitPercent < 0.001 {
			dynamicProfitPercent = 0.001
		} else if dynamicProfitPercent > 0.10 {
			dynamicProfitPercent = 0.10
		}

		dynamicProfitThreshold := 1.0 + dynamicProfitPercent
		targetPrice := pos.Price * dynamicProfitThreshold

		if targetPrice != pos.TargetPrice {
			err := b.db.UpdatePositionTargetPrice(pos.ID, targetPrice)
			if err != nil {
				logger.Errorf("Failed to update target price for position %d: %v", pos.ID, err)
			} else {
				pos.TargetPrice = targetPrice
				logger.Infof("[%s] Position %d, updated TargetPrice → %s (DynamicProfit=%.2f%%, Volatility=%.2f%%)",
					b.Config.ExchangeName, pos.ID, b.market.FormatPrice(pos.TargetPrice), dynamicProfitPercent*100, volatility)
			}
		}

		// Vérifier le profit minimum avec le seuil dynamique
		if currentPrice >= targetPrice {
			// Logique de trailing stop originale : vendre si le prix tombe de 0.1% par rapport au max
			trailingStopThreshold := 1.0 - (b.Config.TrailingStopDelta / 100)
			if currentPrice < (pos.MaxPrice * trailingStopThreshold) {
				logger.Infof("[%s] Position %d, price dropped: %.4f → %.4f, time to sell!",
					b.Config.ExchangeName, pos.ID, pos.MaxPrice, currentPrice)

				b.placeSellOrder(pos, currentPrice)
			}
		}
	}
}

func (b *Bot) handleOrderCheck() {
	logger.Debug("Checking orders...")

	pendingOrders, err := b.db.GetPendingOrders()
	if err != nil {
		logger.Errorf("Failed to get pending orders: %v", err)
		return
	}

	for _, dbOrder := range pendingOrders {
		b.processOrder(dbOrder)
	}
}

func (b *Bot) placeSellOrder(pos database.Position, currentPrice float64) {
	dbCycle, err := b.db.GetCycleForBuyOrderPosition(pos.ID)
	if err != nil {
		logger.Errorf("Failed to get cycle from buy order position %v: %v", pos.ID, err)
	}

	// pour rester maker, on place un ordre juste un peu plus haut que currentPrice, idéalement il faudrait
	// consulter le carnet d'ordre pour se placer juste au-dessus de la meilleure offre
	priceOffset := currentPrice * (b.Config.SellOffset / 100.0)
	limitPrice := b.roundToPrecision(currentPrice+priceOffset, b.market.Precision.Price)

	order, err := b.exchange.PlaceLimitSellOrder(b.Config.Pair, pos.Amount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place Limit Sell Order: %v", err)
		return
	}

	orderPrice := *order.Price
	orderAmount := *order.Amount

	dbOrder, err := b.db.CreateOrder(*order.Id, database.Sell, orderAmount, orderPrice, 0.0, &pos.ID)
	if err != nil {
		logger.Errorf("Failed to save sell order to database: %v", err)
		return
	}

	err = b.db.UpdateCycleSellOrder(dbCycle.ID, dbOrder.ID)
	if err != nil {
		logger.Errorf("Failed to update cycle sell order: %v", err)
	}

	message := ""
	message += fmt.Sprintf("🌀 Cycle on %s [%d] UPDATE", b.Config.ExchangeName, dbCycle.ID)
	message += fmt.Sprintf("\nℹ️ New Sell Order: %d (%s)", dbOrder.ID, *order.Id)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", b.market.FormatAmount(orderAmount), b.market.BaseAsset)
	message += fmt.Sprintf("\n📈 Sell Price: %s %s", b.market.FormatPrice(orderPrice), b.market.QuoteAsset)
	message += fmt.Sprintf("\n💲 Value: %.2f %s", orderAmount*orderPrice, b.market.QuoteAsset)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification to Telegram: %v", err)
	}

	logger.Infof("[%s] Limit Sell Order placed: %f %s at %f %s (ID=%v, DB_ID=%v, Position=%v)",
		b.Config.ExchangeName,
		orderAmount, b.market.BaseAsset, orderPrice, b.market.QuoteAsset, order.Id, dbOrder.ID, pos.ID)
}

func (b *Bot) processOrder(dbOrder database.Order) {
	order, err := b.exchange.FetchOrder(dbOrder.ExternalID, b.Config.Pair)
	if err != nil {
		logger.Errorf("Failed to fetch Order (ID=%v): %v", dbOrder.ExternalID, err)
		return
	}

	if order.Status != nil {
		switch *order.Status {
		case "closed":
			b.handleClosedOrder(dbOrder, order)
		case "canceled":
			b.handleCanceledOrder(dbOrder, order)
		case "open":
			// Removed automatic cancel logic - orders will remain active until filled or manually cancelled
			logger.Debugf("Order %v still open", dbOrder.ExternalID)
		default:
			logger.Warnf("Unsupported Order Status: %v", *order.Status)
		}
	} else {
		logger.Errorf("Order Status is not known")
	}
}

func (b *Bot) handleClosedOrder(dbOrder database.Order, order Order) {
	err := b.db.UpdateOrderStatus(dbOrder.ExternalID, database.Filled)
	if err != nil {
		logger.Errorf("Failed to update order status in database: %v", err)
		return
	}

	switch dbOrder.Side {
	case database.Buy:
		b.handleFilledBuyOrder(dbOrder, order)
	case database.Sell:
		b.handleFilledSellOrder(dbOrder, order)
	}
}

func (b *Bot) handleCanceledOrder(dbOrder database.Order, order Order) {
	err := b.db.UpdateOrderStatus(dbOrder.ExternalID, database.Cancelled)
	if err != nil {
		logger.Errorf("Failed to update order status in database: %v", err)
		return
	}

	message := ""
	message += fmt.Sprintf("🚫 [%s] Order Cancelled: %d (%s)", b.Config.ExchangeName, dbOrder.ID, dbOrder.ExternalID)
	message += fmt.Sprintf("\n💰 Quantity: %s %s\n", b.market.FormatAmount(dbOrder.Amount), b.market.BaseAsset)
	if dbOrder.Side == database.Buy {
		message += fmt.Sprintf("\n📉 Buy Price: %s %s", b.market.FormatPrice(dbOrder.Price), b.market.QuoteAsset)
	} else {
		message += fmt.Sprintf("\n📈 Sell Price: %s %s", b.market.FormatPrice(dbOrder.Price), b.market.QuoteAsset)
	}
	message += fmt.Sprintf("\n💲 Value: %.2f %s", dbOrder.Amount*dbOrder.Price, b.market.QuoteAsset)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification to Telegram: %v", err)
	}

	logger.Infof("[%s] Order %v Cancelled (cancelled manually on exchange)", b.Config.ExchangeName, order.Id)
}

func (b *Bot) handleFilledBuyOrder(dbOrder database.Order, order Order) {
	dbCycle, err := b.db.GetCycleForBuyOrder(dbOrder.ID)
	if err != nil {
		logger.Errorf("Failed to get cycle from buy order %v: %v", dbOrder.ID, err)
	}

	message := ""
	message += fmt.Sprintf("🌀 Cycle on %s [%d] UPDATE", b.Config.ExchangeName, dbCycle.ID)
	message += fmt.Sprintf("\n✅ Buy Order Filled: %s", *order.Id)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", b.market.FormatAmount(*order.Amount), b.market.BaseAsset)
	message += fmt.Sprintf("\n📉 Buy Price: %s %s", b.market.FormatPrice(*order.Price), b.market.QuoteAsset)
	message += fmt.Sprintf("\n💲 Value: %.2f %s", *order.Amount**order.Price, b.market.QuoteAsset)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification to Telegram: %v", err)
	}

	logger.Infof("[%s] Buy Order Filled: %s %s at %s %s (ID=%v)",
		b.Config.ExchangeName,
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
		order.Id)

	// Par défaut, le prix cible est basé sur le profit target configuré
	targetPrice := *order.Price * (1.0 + b.Config.ProfitTarget/100.0)

	position, err := b.db.CreatePosition(*order.Price, targetPrice, *order.Amount)
	if err != nil {
		logger.Errorf("Failed to create position in database: %v", err)
	} else {
		err = b.db.UpdateOrderPosition(dbOrder.ID, position.ID)
		if err != nil {
			logger.Errorf("Failed to update order position in database: %v", err)
		}
		logger.Infof("[%s] Position created (ID=%v, Price=%s, Amount=%s, TargetPrice=%s)",
			b.Config.ExchangeName,
			position.ID,
			b.market.FormatPrice(position.Price),
			b.market.FormatAmount(position.Amount),
			b.market.FormatPrice(position.TargetPrice),
		)
	}
}

func (b *Bot) handleFilledSellOrder(dbOrder database.Order, order Order) {
	dbCycle, err := b.db.GetCycleForSellOrder(dbOrder.ID)
	if err != nil {
		logger.Errorf("Failed to get cycle from sell order %v: %v", dbOrder.ID, err)
	}

	message := ""
	message += fmt.Sprintf("🌀 Cycle on %s [%d] COMPLETE", b.Config.ExchangeName, dbCycle.ID)
	message += fmt.Sprintf("\n✅ [%s] Sell Order Filled: %s", b.Config.ExchangeName, *order.Id)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", b.market.FormatAmount(*order.Amount), b.market.BaseAsset)
	message += fmt.Sprintf("\n📈 Sell Price: %s %s", b.market.FormatPrice(*order.Price), b.market.QuoteAsset)
	message += fmt.Sprintf("\n💲 Value: %.2f %s", *order.Amount**order.Price, b.market.QuoteAsset)

	if dbOrder.PositionID != nil {
		position, err := b.db.GetPosition(*dbOrder.PositionID)
		if err == nil {
			buyValue := position.Price * position.Amount
			sellValue := *order.Amount * *order.Price
			win := sellValue - buyValue
			winPercent := (win / buyValue) * 100
			message += fmt.Sprintf("\n🤑 Profit: %.2f %s (%+.1f%%)", win, b.market.QuoteAsset, winPercent)
		}
	}

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification to Telegram: %v", err)
	}

	logger.Infof("[%s] Sell Order Filled: %s %s at %s %s (ID=%s)",
		b.Config.ExchangeName,
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
		*order.Id)

	if dbOrder.PositionID != nil {
		err := b.db.DeletePosition(*dbOrder.PositionID)
		if err != nil {
			logger.Errorf("Failed to delete position from database: %v", err)
		} else {
			logger.Infof("[%s] Position deleted: ID=%v", b.Config.ExchangeName, *dbOrder.PositionID)
		}
	}
}

func (b *Bot) ShowStatistics() {
	if stats, err := b.db.GetStats(); err == nil {
		logger.Infof("[%s] Positions[Active=%d, Value=%.2f], Orders[Pending=%d, Filled=%d, Cancelled=%d]",
			b.Config.ExchangeName,
			stats["active_positions"],
			stats["total_positions_value"],
			stats["pending_orders"],
			stats["filled_orders"],
			stats["cancelled_orders"],
		)
	}
}

func (b *Bot) roundToPrecision(value, precision float64) float64 {
	factor := 1 / precision
	return float64(int64(value*factor)) / factor
}

func (m *Market) FormatAmount(amount float64) string {
	return strconv.FormatFloat(amount, 'f', m.Precision.AmountDecimals, 64)
}

func (m *Market) FormatPrice(price float64) string {
	return strconv.FormatFloat(price, 'f', m.Precision.PriceDecimals, 64)
}

// calculateVolatility calcule la volatilité quotidienne à partir des prix de clôture
func (b *Bot) CalculateVolatility() (float64, error) {
	period := b.Config.VolatilityPeriod
	since := time.Now().AddDate(0, 0, -period).UnixMilli()
	candles, err := b.exchange.FetchCandles(b.Config.Pair, "4h", &since, int64(period*6))
	if err != nil {
		logger.Errorf("Failed to fetch OHLCV data: %v", err)
		return 0, err
	}

	// Extraire les prix de clôture (index 4 dans chaque kline)
	prices := make([]float64, len(candles))
	for i, candle := range candles {
		prices[i] = candle.Close
	}

	if len(prices) < 2 {
		return 0, fmt.Errorf("not enough price data for volatility calculation")
	}

	// Calculer les rendements quotidiens
	returns := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		returns[i-1] = (prices[i] - prices[i-1]) / prices[i-1]
	}

	// Calculer la moyenne des rendements
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Calculer la variance
	var variance float64
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	variance /= float64(len(returns))

	// Volatilité = écart-type (racine carrée de la variance)
	volatility := math.Sqrt(variance)
	return volatility * 100, nil // Convertir en pourcentage
}

// calculateRSI calcule l'indice de force relative (RSI) pour une période donnée
func (b *Bot) CalculateRSI() (float64, error) {
	// Récupérer suffisamment de données pour le calcul RSI
	period := b.Config.RSIPeriod
	since := time.Now().AddDate(0, 0, -period).UnixMilli()
	candles, err := b.exchange.FetchCandles(b.Config.Pair, "4h", &since, 500)
	if err != nil {
		logger.Errorf("Failed to fetch candles for RSI: %v", err)
		return 0, err
	}

	if len(candles) < (period + 1) {
		return 0, fmt.Errorf("not enough candle data for RSI calculation")
	}

	// Calculer les gains et pertes
	gains := make([]float64, len(candles)-1)
	losses := make([]float64, len(candles)-1)

	for i := 1; i < len(candles); i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains[i-1] = change
			losses[i-1] = 0
		} else {
			gains[i-1] = 0
			losses[i-1] = -change
		}
	}

	// Calculer les moyennes mobiles exponentielles des gains et pertes
	avgGain := gains[0]
	avgLoss := losses[0]

	for i := 1; i < len(gains); i++ {
		avgGain = (avgGain*(float64(period)-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*(float64(period)-1) + losses[i]) / float64(period)
	}

	// Calculer le RSI
	if avgLoss == 0 {
		return 100, nil
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi, nil
}

// ===============================
// EXCHANGE ADAPTER FOR MARKET DATA
// ===============================

// botExchangeAdapter adapts the bot's Exchange interface to work with market.Exchange
type botExchangeAdapter struct {
	exchange Exchange
}

func newBotExchangeAdapter(exchange Exchange) market.Exchange {
	return &botExchangeAdapter{exchange: exchange}
}

func (bea *botExchangeAdapter) FetchCandles(pair string, timeframe string, since *int64, limit int64) ([]market.BotCandle, error) {
	// Call the bot's exchange FetchCandles method
	botCandles, err := bea.exchange.FetchCandles(pair, timeframe, since, limit)
	if err != nil {
		return nil, err
	}

	// Convert bot.Candle to market.BotCandle
	marketCandles := make([]market.BotCandle, len(botCandles))
	for i, candle := range botCandles {
		marketCandles[i] = market.BotCandle{
			Timestamp: candle.Timestamp,
			Open:      candle.Open,
			High:      candle.High,
			Low:       candle.Low,
			Close:     candle.Close,
			Volume:    candle.Volume,
		}
	}

	return marketCandles, nil
}

func (bea *botExchangeAdapter) GetPrice(pair string) (float64, error) {
	return bea.exchange.GetPrice(pair)
}
