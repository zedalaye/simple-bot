package bot

import (
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
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

type Candle struct {
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

type Bot struct {
	config   config.BotConfig
	db       *database.DB
	exchange Exchange
	market   Market
	done     chan bool
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
	b.market = b.exchange.GetMarket(b.config.Pair)

	logger.Infof("Base Asset: %s, Quote Asset: %s", b.market.BaseAsset, b.market.QuoteAsset)
	logger.Infof("Market precision: price=%f, amount=%f", b.market.Precision.Price, b.market.Precision.Amount)
	return nil
}

func (b *Bot) Start(buyAtLaunch bool) error {
	logger.Info("Starting bot...")
	b.ShowStatistics()

	b.handleOrderCheck()
	b.handlePriceCheck()

	if buyAtLaunch {
		b.handleBuySignal()
	}

	logger.Debug("Starting bot goroutine...")
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

	checkTicker := time.NewTicker(b.config.CheckInterval)
	defer checkTicker.Stop()

	for {
		select {
		case <-b.done:
			logger.Info("Bot stopped gracefully")
			return
		case <-buyTicker.C:
			b.handleBuySignal()
		case <-checkTicker.C:
			b.handlePriceCheck()
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

	quoteAssetBalance, ok := balance[b.market.QuoteAsset]
	if !ok || (quoteAssetBalance.Free < b.config.QuoteAmount) {
		logger.Warnf("USDC balance not found or insufficient: %v", quoteAssetBalance.Free)
		return
	}

	currentPrice, err := b.exchange.GetPrice(b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to get ticker data: %v", err)
		return
	}

	limitPrice := b.roundToPrecision(currentPrice-b.config.PriceOffset, b.market.Precision.Price)
	baseAmount := b.roundToPrecision(b.config.QuoteAmount/limitPrice, b.market.Precision.Amount)

	order, err := b.exchange.PlaceLimitBuyOrder(b.config.Pair, baseAmount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place Limit Buy Order: %v", err)
		return
	}

	orderPrice := *order.Price
	orderAmount := *order.Amount

	dbOrder, err := b.db.CreateOrder(*order.Id, database.Buy, orderAmount, orderPrice, nil)
	if err != nil {
		logger.Errorf("Failed to save buy order to database: %v", err)
		return
	}

	telegram.SendMessage(fmt.Sprintf("Buy Order : %s %s at %s %s",
		b.market.FormatAmount(orderAmount), b.market.BaseAsset, b.market.FormatPrice(orderPrice), b.market.QuoteAsset,
	))

	logger.Infof("Limit Buy Order placed: %s %s at %s %s (ID=%v, DB_ID=%v)",
		b.market.FormatAmount(orderAmount), b.market.BaseAsset, b.market.FormatPrice(orderPrice), b.market.QuoteAsset,
		order.Id, dbOrder.ID)
}

func (b *Bot) handlePriceCheck() {
	logger.Debug("Checking prices...")

	currentPrice, err := b.exchange.GetPrice(b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to get ticker data: %v", err)
		return
	}

	currentPrice = b.roundToPrecision(currentPrice, b.market.Precision.Price)
	logger.Infof("Current price: %v", currentPrice)

	positions, err := b.db.GetOpenPositions()
	if err != nil {
		logger.Errorf("Failed to get open positions from database: %v", err)
		return
	}
	logger.Debugf("Found %d open positions", len(positions))

	trailingOffset := b.config.PriceOffset         // 200 $
	minProfitThreshold := b.config.ProfitThreshold // 1.02

	for _, pos := range positions {
		// Mettre à jour le prix maximum observé
		if currentPrice > pos.MaxPrice {
			err := b.db.UpdatePositionMaxPrice(pos.ID, currentPrice)
			if err != nil {
				logger.Errorf("Failed to update max price for position %v: %v", pos.ID, err)
				continue
			}
			pos.MaxPrice = currentPrice
			logger.Infof("Updated max price for position %v to %v", pos.ID, pos.MaxPrice)
		}

		// Vérifier le profit minimum
		if currentPrice >= pos.Price*minProfitThreshold {
			// Vendre si le prix tombe en dessous de maxPrice - trailingOffset
			if currentPrice <= pos.MaxPrice-trailingOffset {
				b.placeSellOrder(pos, currentPrice)
			}
		}
	}

	b.ShowStatistics()
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

	b.ShowStatistics()
}

func (b *Bot) placeSellOrder(pos database.Position, currentPrice float64) {
	limitPrice := b.roundToPrecision(currentPrice+b.config.PriceOffset, b.market.Precision.Price)
	order, err := b.exchange.PlaceLimitSellOrder(b.config.Pair, pos.Amount, limitPrice)
	if err != nil {
		logger.Errorf("Failed to place Limit Sell Order: %v", err)
		return
	}

	orderPrice := *order.Price
	orderAmount := *order.Amount

	dbOrder, err := b.db.CreateOrder(*order.Id, database.Sell, orderAmount, orderPrice, &pos.ID)
	if err != nil {
		logger.Errorf("Failed to save sell order to database: %v", err)
		return
	}

	telegram.SendMessage(fmt.Sprintf("Sell Order : %f %s at %f %s",
		orderAmount, b.market.BaseAsset, orderPrice, b.market.QuoteAsset,
	))

	logger.Infof("Limit Sell Order placed: %f %s at %f %s (ID=%v, DB_ID=%v, Position=%v)",
		orderAmount, b.market.BaseAsset, orderPrice, b.market.QuoteAsset, order.Id, dbOrder.ID, pos.ID)
}

func (b *Bot) processOrder(dbOrder database.Order) {
	order, err := b.exchange.FetchOrder(dbOrder.ExternalID, b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to fetch Order (ID=%v): %v", dbOrder.ExternalID, err)
		return
	}

	if order.Status != nil {
		if *order.Status == "closed" {
			b.handleClosedOrder(dbOrder, order)
		} else if *order.Status == "canceled" {
			b.handleCanceledOrder(dbOrder, order)
		} else if *order.Status == "open" {
			if b.shouldCancelOrder(order) {
				b.handleCancelOrder(dbOrder)
			}
		} else {
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
		b.handleFilledBuyOrder(order)
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

	telegram.SendMessage(fmt.Sprintf("Order %s Cancelled (manually on exchange) : %s %s at %s %s",
		*order.Id,
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
	))

	logger.Infof("Order %v Cancelled (cancelled manually on exchange)", order.Id)
}

func (b *Bot) handleFilledBuyOrder(order Order) {
	telegram.SendMessage(fmt.Sprintf("Buy Order Filled : %s %s at %s %s",
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
	))

	logger.Infof("Buy Order Filled: %s %s at %s %s (ID=%v)",
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
		order.Id)

	position, err := b.db.CreatePosition(*order.Price, *order.Amount)
	if err != nil {
		logger.Errorf("Failed to create position in database: %v", err)
	} else {
		logger.Infof("Position created: ID=%v, Price=%v, Amount=%v",
			position.ID, position.Price, position.Amount)
	}
}

func (b *Bot) handleFilledSellOrder(dbOrder database.Order, order Order) {
	telegram.SendMessage(fmt.Sprintf("Sell Order Filled : %s %s at %s %s",
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
	))

	logger.Infof("Sell Order Filled: %s %s at %s %s (ID=%s)",
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
		*order.Id)

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
	order, err := b.exchange.CancelOrder(dbOrder.ExternalID, b.config.Pair)
	if err != nil {
		logger.Errorf("Failed to Cancel Order (ID=%v): %v", dbOrder.ExternalID, err)
		return
	}

	telegram.SendMessage(fmt.Sprintf("Order %s Cancelled (too old) : %s %s at %s %s",
		*order.Id,
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
	))

	logger.Infof("Order %v Cancelled (too old)", dbOrder.ExternalID)

	err = b.db.UpdateOrderStatus(dbOrder.ExternalID, database.Cancelled)
	if err != nil {
		logger.Errorf("Failed to update cancelled order status in database: %v", err)
	}

	if dbOrder.Side == database.Sell && dbOrder.PositionID != nil {
		logger.Infof("Sell order cancelled - Position %v remains active", *dbOrder.PositionID)
	}
}

func (b *Bot) ShowStatistics() {
	if stats, err := b.db.GetStats(); err == nil {
		logger.Infof("Statistics: Positions[Active=%d, Value=%.2f], Orders[Pending=%d, Filled=%d, Cancelled=%d]",
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

// getDailyPrices récupère les prix de clôture quotidiens pour une période donnée
func (b *Bot) getDailyPrices(pair string, limit int64) ([]float64, error) {
	candles, err := b.exchange.FetchCandles(pair, "1d", nil, limit)
	if err != nil {
		logger.Errorf("Failed to fetch OHLCV data: %v", err)
		return nil, err
	}

	// Extraire les prix de clôture (index 4 dans chaque kline)
	prices := make([]float64, len(candles))
	for i, candle := range candles {
		prices[i] = candle.Close
	}
	return prices, nil
}

// calculateVolatility calcule la volatilité quotidienne à partir des prix de clôture
func (b *Bot) calculateVolatility(pair string, period int64) (float64, error) {
	prices, err := b.getDailyPrices(pair, period)
	if err != nil {
		return 0, err
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
