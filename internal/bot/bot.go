package bot

import (
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
	"time"
)

type Exchange interface {
	GetMarket(pair string) (Market, error)
	FetchBalance() (map[string]Balance, error)
	PlaceLimitBuyOrder(pair string, amount float64, price float64) (Order, error)
	PlaceLimitSellOrder(pair string, amount float64, price float64) (Order, error)
	FetchOrder(id string, symbol string) (Order, error)
	CancelOrder(id string, symbol string) (Order, error)
	GetPrice(pair string) (float64, error)
}

type Market struct {
	Symbol    string
	BaseId    string
	QuoteId   string
	Precision struct {
		Price  float64
		Amount float64
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

type Bot struct {
	config          config.BotConfig
	db              *database.DB
	exchange        Exchange
	pricePrecision  float64
	amountPrecision float64
	baseAsset       string
	quoteAsset      string
	done            chan bool
}

func NewBot(config config.BotConfig, db *database.DB, exchange Exchange) (*Bot, error) {
	bot := &Bot{
		config:   config,
		db:       db,
		exchange: exchange,
		done:     make(chan bool),
	}

	if err := bot.initializeMarketPrecision(); err != nil {
		return nil, err
	}

	return bot, nil
}

func (b *Bot) initializeMarketPrecision() error {
	logger.Info("Fetching market data...")
	market, err := b.exchange.GetMarket(b.config.Pair)
	if err != nil {
		return err
	}

	b.pricePrecision = 0.01
	b.amountPrecision = 0.000001

	b.baseAsset = market.BaseId
	b.quoteAsset = market.QuoteId
	b.pricePrecision = market.Precision.Price
	b.amountPrecision = market.Precision.Amount

	logger.Infof("Base Asset: %s, Quote Asset: %s", b.baseAsset, b.quoteAsset)
	logger.Infof("Market precision: price=%v, amount=%v", b.pricePrecision, b.amountPrecision)
	return nil
}

func (b *Bot) Start() error {
	logger.Info("Starting bot...")
	if stats, err := b.db.GetStats(); err == nil {
		logger.Infof("Bot statistics: %+v", stats)
	}
	go b.run()
	return nil
}

func (b *Bot) Stop() {
	logger.Info("Stopping bot...")
	close(b.done)
}

func (b *Bot) run() {
	buyTicker := time.NewTicker(b.config.BuyInterval)
	defer buyTicker.Stop()

	priceCheckTicker := time.NewTicker(b.config.CheckInterval)
	defer priceCheckTicker.Stop()

	orderCheckTicker := time.NewTicker(b.config.CheckInterval)
	defer orderCheckTicker.Stop()

	for {
		select {
		case <-b.done:
			logger.Info("Bot stopped gracefully")
			return
		case <-buyTicker.C:
			b.handleBuySignal()
		case <-priceCheckTicker.C:
			b.handlePriceCheck()
		case <-orderCheckTicker.C:
			b.handleOrderCheck()
		}
	}
}

func (b *Bot) handleBuySignal() {
	logger.Info("Time to place a new Buy Order...")

	balance, err := b.exchange.FetchBalance()
	if err != nil {
		logger.Errorf("Failed to fetch balances: %v", err)
		return
	}

	quoteAssetBalance, ok := balance[b.quoteAsset]
	if !ok || (quoteAssetBalance.Free < b.config.QuoteAmount) {
		logger.Warnf("USDC balance not found or insufficient: %v", quoteAssetBalance.Free)
		return
	}

	currentPrice, err := b.exchange.GetPrice(b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to get ticker data: %v", err)
		return
	}

	limitPrice := currentPrice - b.config.PriceOffset
	baseAmount := b.config.QuoteAmount / limitPrice

	order, err := b.exchange.PlaceLimitBuyOrder(b.config.Pair, baseAmount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place Limit Buy Order: %v", err)
		return
	}

	orderPrice := b.roundToPrecision(*order.Price, b.pricePrecision)
	orderAmount := b.roundToPrecision(*order.Amount, b.amountPrecision)

	dbOrder, err := b.db.CreateOrder(*order.Id, database.Buy, orderAmount, orderPrice, nil)
	if err != nil {
		logger.Errorf("Failed to save buy order to database: %v", err)
		return
	}

	logger.Infof("Limit Buy Order placed: %v %s at %v %s (ID=%v, DB_ID=%v)",
		orderAmount, b.baseAsset, orderPrice, b.quoteAsset, order.Id, dbOrder.ID)
}

func (b *Bot) handlePriceCheck() {
	logger.Debug("Checking prices...")

	currentPrice, err := b.exchange.GetPrice(b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to get ticker data: %v", err)
		return
	}

	currentPrice = b.roundToPrecision(currentPrice, b.pricePrecision)
	logger.Debugf("Current price: %v", currentPrice)

	positions, err := b.db.GetAllPositions()
	if err != nil {
		logger.Errorf("Failed to get positions from database: %v", err)
		return
	}

	for _, pos := range positions {
		if currentPrice >= pos.Price*b.config.ProfitThreshold {
			b.placeSellOrder(pos, currentPrice)
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

	if stats, err := b.db.GetStats(); err == nil {
		logger.Debugf("Bot statistics: %+v", stats)
	}
}

func (b *Bot) placeSellOrder(pos database.Position, currentPrice float64) {
	limitPrice := currentPrice + b.config.PriceOffset
	order, err := b.exchange.PlaceLimitSellOrder(b.config.Pair, pos.Amount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place Limit Sell Order: %v", err)
		return
	}

	orderPrice := b.roundToPrecision(*order.Price, b.pricePrecision)
	orderAmount := b.roundToPrecision(*order.Amount, b.amountPrecision)

	dbOrder, err := b.db.CreateOrder(*order.Id, database.Sell, orderAmount, orderPrice, &pos.ID)
	if err != nil {
		logger.Errorf("Failed to save sell order to database: %v", err)
		return
	}

	logger.Infof("Limit Sell Order placed: %v %s at %v %s (ID=%v, DB_ID=%v, Position=%v)",
		orderAmount, b.baseAsset, orderPrice, b.quoteAsset, order.Id, dbOrder.ID, pos.ID)
}

func (b *Bot) processOrder(dbOrder database.Order) {
	order, err := b.exchange.FetchOrder(dbOrder.ExternalID, b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to fetch Order (ID=%v): %v", dbOrder.ExternalID, err)
		return
	}

	if order.Status != nil {
		if *order.Status == "FILLED" {
			b.handleFilledOrder(dbOrder, order)
		} else if b.shouldCancelOrder(order) {
			b.handleCancelOrder(dbOrder)
		}
	} else {
		logger.Errorf("Order Status is not known")
	}
}

func (b *Bot) handleFilledOrder(dbOrder database.Order, order Order) {
	err := b.db.UpdateOrderStatus(dbOrder.ExternalID, database.Filled)
	if err != nil {
		logger.Errorf("Failed to update order status in database: %v", err)
		return
	}

	switch dbOrder.Side {
	case database.Buy:
		b.handleFilledBuyOrder(order)
	case database.Sell:
		b.handleFilledSellOrder(dbOrder, order)
	}
}

func (b *Bot) handleFilledBuyOrder(order Order) {
	logger.Infof("Buy Order Filled: %v %s at %v %s (ID=%v)",
		order.Amount, b.baseAsset, order.Price, b.quoteAsset, order.Id)

	position, err := b.db.CreatePosition(
		b.roundToPrecision(*order.Price, b.pricePrecision),
		b.roundToPrecision(*order.Amount, b.amountPrecision),
	)
	if err != nil {
		logger.Errorf("Failed to create position in database: %v", err)
	} else {
		logger.Infof("Position created: ID=%v, Price=%v, Amount=%v",
			position.ID, position.Price, position.Amount)
	}
}

func (b *Bot) handleFilledSellOrder(dbOrder database.Order, order Order) {
	logger.Infof("Sell Order Filled: %v %s at %v %s (ID=%v)",
		order.Amount, b.baseAsset, order.Price, b.quoteAsset, order.Id)

	if dbOrder.PositionID != nil {
		err := b.db.DeletePosition(*dbOrder.PositionID)
		if err != nil {
			logger.Errorf("Failed to delete position from database: %v", err)
		} else {
			logger.Infof("Position deleted: ID=%v", *dbOrder.PositionID)
		}
	}
}

func (b *Bot) shouldCancelOrder(order Order) bool {
	return (order.Timestamp != nil) && (*order.Timestamp > 0) &&
		time.Since(time.UnixMilli(*order.Timestamp)) > b.config.OrderTTL
}

func (b *Bot) handleCancelOrder(dbOrder database.Order) {
	_, err := b.exchange.CancelOrder(dbOrder.ExternalID, b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to Cancel Order (ID=%v): %v", dbOrder.ExternalID, err)
		return
	}

	logger.Infof("Order %v Cancelled (too old)", dbOrder.ExternalID)

	err = b.db.UpdateOrderStatus(dbOrder.ExternalID, database.Cancelled)
	if err != nil {
		logger.Errorf("Failed to update cancelled order status in database: %v", err)
	}

	if dbOrder.Side == database.Sell && dbOrder.PositionID != nil {
		logger.Infof("Sell order cancelled - Position %v remains active", *dbOrder.PositionID)
	}
}

func (b *Bot) ShowFinalStats() {
	if stats, err := b.db.GetStats(); err == nil {
		logger.Infof("Final statistics: %+v", stats)
	}
}

func (b *Bot) roundToPrecision(value, precision float64) float64 {
	factor := 1 / precision
	return float64(int64(value*factor)) / factor
}
