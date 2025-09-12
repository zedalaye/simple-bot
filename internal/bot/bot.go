package bot

import (
	"bot/internal/algorithms"
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"
	"bot/internal/scheduler"
	"bot/internal/telegram"
	"fmt"
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
	Calculator        *market.Calculator
	algorithmRegistry *algorithms.AlgorithmRegistry
	strategyScheduler *scheduler.StrategyScheduler
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
	bot.marketCollector = market.NewMarketDataCollector(config.Pair, db, exchangeAdapter)
	bot.Calculator = market.NewCalculator(db, bot.marketCollector)

	// Initialize algorithm registry
	bot.algorithmRegistry = algorithms.NewAlgorithmRegistry()
	logger.Infof("[%s] Algorithm registry initialized with %d algorithms", config.ExchangeName, len(bot.algorithmRegistry.List()))

	strategyScheduler, err := scheduler.NewStrategyScheduler(config.Pair, db, bot.marketCollector, bot.Calculator, bot.algorithmRegistry, bot)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy scheduler: %w", err)
	}
	bot.strategyScheduler = strategyScheduler
	logger.Infof("[%s] ✓ Initialized Strategy Scheduler with %d strategies", config.ExchangeName, len(bot.strategyScheduler.List()))

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

	// buyAtLaunch is now handled by the strategy scheduler
	logger.Debug("Starting cron-based strategy scheduler...")
	go b.run()
	return nil
}

func (b *Bot) Stop() {
	logger.Infof("[%s] Stopping bot...", b.Config.ExchangeName)
	close(b.done)
}

func (b *Bot) run() {
	logger.Info("🚀 Starting cron-based strategy scheduler...")

	// Start the strategy scheduler with real cron
	err := b.strategyScheduler.Start()
	if err != nil {
		logger.Fatalf("Failed to start strategy scheduler: %v", err)
		return
	}

	// Strategy scheduler manages buy signals, we handle ongoing position/order management
	checkTicker := time.NewTicker(b.Config.CheckInterval)
	defer checkTicker.Stop()

	logger.Info("✅ Cron scheduler active, strategies will execute according to their cron expressions")

	for {
		select {
		case <-b.done:
			logger.Infof("[%s] Bot stopping gracefully", b.Config.ExchangeName)
			// Stop the strategy scheduler
			if b.strategyScheduler != nil {
				err := b.strategyScheduler.Stop()
				if err != nil {
					logger.Errorf("Failed to stop strategy scheduler: %v", err)
				}
			}
			return
		case <-checkTicker.C:
			b.handlePriceCheck() // Update position max prices + trailing stop
			b.handleOrderCheck() // Check pending orders status
			b.ShowStatistics()
		}
	}
}

// All buy logic is now handled by the strategy scheduler
// No more demo methods or legacy buy signal handling needed

func (b *Bot) handlePriceCheck() {
	logger.Debug("Checking prices and positions...")

	currentPrice, err := b.exchange.GetPrice(b.Config.Pair)
	if err != nil {
		logger.Errorf("Failed to get current price: %v", err)
		return
	}

	currentPrice = b.roundToPrecision(currentPrice, b.market.Precision.Price)
	logger.Infof("[%s] Current price: %s", b.Config.ExchangeName, b.market.FormatPrice(currentPrice))

	// Get all open positions (from all strategies)
	cycles, err := b.db.GetOpenCycles()
	if err != nil {
		logger.Errorf("Failed to get open cycles: %v", err)
		return
	}
	logger.Debugf("Checking %d open cycles for sell signals", len(cycles))

	for _, cycle := range cycles {
		// Update max price if current price is higher
		if currentPrice > cycle.MaxPrice {
			err := b.db.UpdateCycleMaxPrice(cycle.ID, currentPrice)
			if err != nil {
				logger.Errorf("Failed to update max price for cycle %d: %v", cycle.ID, err)
				continue
			}
			cycle.MaxPrice = currentPrice
			logger.Infof("[%s] Cycle %d updated MaxPrice → %s",
				b.Config.ExchangeName, cycle.ID, b.market.FormatPrice(cycle.MaxPrice))
		}

		// Check trailing stop using PRE-CALCULATED target price (no more dynamic recalculation!)
		if currentPrice >= cycle.TargetPrice {
			// Get strategy for this position to get trailing stop settings
			strategy, err := b.db.GetStrategy(cycle.StrategyID)
			if err != nil {
				logger.Errorf("Failed to get strategy for cycle %d: %v", cycle.ID, err)
				continue
			}

			trailingStopThreshold := 1.0 - (strategy.TrailingStopDelta / 100)
			if currentPrice < (cycle.MaxPrice * trailingStopThreshold) {
				logger.Infof("[%s] Position %d trailing stop triggered: %.4f < %.4f",
					b.Config.ExchangeName, cycle.ID, currentPrice, cycle.MaxPrice*trailingStopThreshold)

				b.placeSellOrderLegacy(cycle, currentPrice)
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

func (b *Bot) placeSellOrderLegacy(cycle database.CycleEnhanced, currentPrice float64) {
	// pour rester maker, on place un ordre juste un peu plus haut que currentPrice, idéalement il faudrait
	// consulter le carnet d'ordre pour se placer juste au-dessus de la meilleure offre
	priceOffset := currentPrice * (b.Config.SellOffset / 100.0)
	limitPrice := b.roundToPrecision(currentPrice+priceOffset, b.market.Precision.Price)

	order, err := b.exchange.PlaceLimitSellOrder(b.Config.Pair, cycle.BuyOrder.Amount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place Limit Sell Order: %v", err)
		return
	}

	orderPrice := *order.Price
	orderAmount := *order.Amount

	dbOrder, err := b.db.CreateOrder(*order.Id, database.Sell, orderAmount, orderPrice, 0.0, cycle.StrategyID)
	if err != nil {
		logger.Errorf("Failed to save sell order to database: %v", err)
		return
	}

	err = b.db.UpdateCycleSellOrder(cycle.ID, dbOrder.ID)
	if err != nil {
		logger.Errorf("Failed to update cycle sell order: %v", err)
	}

	message := ""
	message += fmt.Sprintf("🌀 Cycle on %s [%d] UPDATE", b.Config.ExchangeName, cycle.ID)
	message += fmt.Sprintf("\nℹ️ New Sell Order: %d (%s)", dbOrder.ID, *order.Id)
	message += fmt.Sprintf("\n💰 Quantity: %s %s", b.market.FormatAmount(orderAmount), b.market.BaseAsset)
	message += fmt.Sprintf("\n📈 Sell Price: %s %s", b.market.FormatPrice(orderPrice), b.market.QuoteAsset)
	message += fmt.Sprintf("\n💲 Value: %.2f %s", orderAmount*orderPrice, b.market.QuoteAsset)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification to Telegram: %v", err)
	}

	logger.Infof("[%s] Limit Sell Order placed: %f %s at %f %s (ID=%v, DB_ID=%v, Cycle=%v)",
		b.Config.ExchangeName,
		orderAmount, b.market.BaseAsset, orderPrice, b.market.QuoteAsset, order.Id, dbOrder.ID, cycle.ID)
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

	// Mais il peut-être différent selon la stratégie
	strategy, err := b.db.GetStrategy(dbCycle.StrategyID)
	if err == nil {
		targetPrice = *order.Price * (1.0 + strategy.ProfitTarget/100.0)
	} else {
		logger.Errorf("Failed to get strategy for cycle : %v", err)
	}

	err = b.db.UpdateCycleTargetPrice(dbCycle.ID, targetPrice)
	if err != nil {
		logger.Errorf("Failed to update cycle target price in database: %v", err)
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

	buyValue := dbCycle.BuyOrder.Price * dbCycle.BuyOrder.Amount
	win := *dbCycle.Profit
	winPercent := (win / buyValue) * 100
	message += fmt.Sprintf("\n🤑 Profit: %.2f %s (%+.1f%%)", win, b.market.QuoteAsset, winPercent)

	err = telegram.SendMessage(message)
	if err != nil {
		logger.Errorf("Failed to send notification to Telegram: %v", err)
	}

	logger.Infof("[%s] Sell Order Filled: %s %s at %s %s (ID=%s)",
		b.Config.ExchangeName,
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
		*order.Id)
}

func (b *Bot) ShowStatistics() {
	if stats, err := b.db.GetStats(); err == nil {
		logger.Infof("[%s] Profit[Average=%.2f, Total=%.2f] Cycles[Active=%d, Completed=%d, Count=%d] Orders[Pending=%d, Filled=%d, Cancelled=%d]",
			b.Config.ExchangeName,

			stats["average_profit"],
			stats["total_profit"],

			stats["active_cycles_count"],
			stats["completed_cycles_count"],
			stats["cycles_count"],

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

// Legacy calculation methods removed - now using Calculator with indicator v2 channels

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

// ===============================
// STRATEGY EXCHANGE IMPLEMENTATION FOR BOT
// ===============================

// Implement scheduler.StrategyExchange interface directly in Bot
func (b *Bot) FetchBalance() (map[string]scheduler.ExchangeBalance, error) {
	botBalance, err := b.exchange.FetchBalance()
	if err != nil {
		return nil, err
	}

	balance := make(map[string]scheduler.ExchangeBalance)
	for asset, bal := range botBalance {
		balance[asset] = scheduler.ExchangeBalance{Free: bal.Free}
	}

	return balance, nil
}

func (b *Bot) GetPrice(pair string) (float64, error) {
	return b.exchange.GetPrice(pair)
}

func (b *Bot) PlaceLimitBuyOrder(pair string, amount float64, price float64) (scheduler.ExchangeOrder, error) {
	// Round according to market precision
	amount = b.roundToPrecision(amount, b.market.Precision.Amount)
	price = b.roundToPrecision(price, b.market.Precision.Price)

	botOrder, err := b.exchange.PlaceLimitBuyOrder(pair, amount, price)
	if err != nil {
		return scheduler.ExchangeOrder{}, err
	}

	return scheduler.ExchangeOrder{
		Id:     botOrder.Id,
		Price:  botOrder.Price,
		Amount: botOrder.Amount,
		Status: botOrder.Status,
	}, nil
}

func (b *Bot) PlaceLimitSellOrder(pair string, amount float64, price float64) (scheduler.ExchangeOrder, error) {
	// Round according to market precision
	amount = b.roundToPrecision(amount, b.market.Precision.Amount)
	price = b.roundToPrecision(price, b.market.Precision.Price)

	botOrder, err := b.exchange.PlaceLimitSellOrder(pair, amount, price)
	if err != nil {
		return scheduler.ExchangeOrder{}, err
	}

	return scheduler.ExchangeOrder{
		Id:     botOrder.Id,
		Price:  botOrder.Price,
		Amount: botOrder.Amount,
		Status: botOrder.Status,
	}, nil
}
