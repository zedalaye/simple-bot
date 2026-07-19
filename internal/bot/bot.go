package bot

import (
	"bot/internal/algorithms"
	"bot/internal/api"
	"bot/internal/core/config"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"
	"bot/internal/notify"
	"bot/internal/scheduler"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// healthClient sert les pings « dead-man's switch » (timeout court, non bloquant).
var healthClient = &http.Client{Timeout: 10 * time.Second}

// errorAlertCooldown : délai minimum entre deux notifications d'erreur, pour
// éviter le spam quand une même erreur se répète à chaque tick.
const errorAlertCooldown = 30 * time.Minute

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
	Free  float64
	Used  float64 // bloqué dans des ordres ouverts
	Total float64 // Free + Used
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
	// paused suspend le déclenchement de nouvelles stratégies (achats et ventes).
	// La surveillance des ordres et le suivi du prix max continuent de tourner.
	paused atomic.Bool
	// startedAt : heure de démarrage du bot ; lastCheck : horodatage (unix nanos)
	// du dernier price-check réussi. Servent au heartbeat affiché dans /status.
	startedAt time.Time
	lastCheck atomic.Int64
	// lastPatternCandleTs : timestamp (ms) de la dernière bougie 1h déjà évaluée par le
	// moniteur de retournement, pour ne notifier qu'une fois par bougie.
	lastPatternCandleTs atomic.Int64
	// État du throttling des alertes d'erreur (accédé uniquement depuis run()).
	lastAlertCount int64
	lastAlertAt    time.Time
	// notifier diffuse les événements (achat/vente rempli, pattern, erreurs) vers
	// les canaux configurés. Jamais nil : Nop par défaut, injecté par SetNotifier.
	notifier notify.Notifier
}

// SetNotifier branche le ou les canaux de notification. Appelé au démarrage du
// daemon ; sans appel, le bot ne notifie rien (utile pour les outils offline).
func (b *Bot) SetNotifier(n notify.Notifier) {
	if n != nil {
		b.notifier = n
	}
}

// emit diffuse un événement sans jamais interrompre le trading : un canal en
// échec est logué, rien de plus.
func (b *Bot) emit(e notify.Event) {
	e.At = time.Now()
	if err := b.notifier.Notify(e); err != nil {
		logger.Errorf("[%s] Échec de diffusion de la notification (%s) : %v",
			b.Config.ExchangeName, e.Kind, err)
	}
}

func NewBot(config config.BotConfig, db *database.DB, exchange Exchange) (*Bot, error) {
	bot := &Bot{
		Config:   config,
		db:       db,
		exchange: exchange,
		done:     make(chan bool),
		notifier: notify.Nop(),
	}

	if err := bot.initializeMarketPrecision(); err != nil {
		return nil, err
	}

	// Initialize market data services with adapter
	exchangeAdapter := newBotExchangeAdapter(exchange)
	bot.marketCollector = market.NewMarketDataCollector(config.ExchangeName, config.Pair, db, exchangeAdapter)
	bot.Calculator = market.NewCalculator(db, bot.marketCollector)

	// Initialize algorithm registry
	bot.algorithmRegistry = algorithms.NewAlgorithmRegistry()
	logger.Infof("[%s] Algorithm registry initialized with %d algorithms", config.ExchangeName, len(bot.algorithmRegistry.List()))

	strategyScheduler, err := scheduler.NewStrategyScheduler(config.ExchangeName, config.Pair, db, &bot.market, bot.marketCollector, bot.Calculator, bot.algorithmRegistry, bot)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy scheduler: %w", err)
	}
	bot.strategyScheduler = strategyScheduler
	logger.Infof("[%s] ✓ Initialized Strategy Scheduler with %d strategies", config.ExchangeName, len(bot.strategyScheduler.List()))

	// Initial market data collection
	logger.Infof("[%s] Initializing market data collection...", config.ExchangeName)
	err = bot.marketCollector.CollectCandles(config.Pair, "4h", 200) // Collect initial historical data
	if err != nil {
		logger.Warnf("[%s] Failed to collect initial market data: %v", config.ExchangeName, err)
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

func (b *Bot) ExchangeName() string {
	return b.Config.ExchangeName
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

	b.startedAt = time.Now()
	b.handleOrderCheck()
	b.handlePriceCheck()
	b.executeBuyStrategies()
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

// Pause suspend le déclenchement de nouvelles stratégies (achats périodiques,
// achats cron via l'arrêt du scheduler, et ventes). La surveillance des ordres
// en cours et le suivi du prix max continuent de tourner.
func (b *Bot) Pause() error {
	if b.paused.Swap(true) {
		return nil // déjà en pause
	}

	logger.Infof("[%s] ⏸ Bot mis en pause (achats/ventes suspendus)", b.Config.ExchangeName)
	if b.strategyScheduler != nil {
		if err := b.strategyScheduler.Stop(); err != nil {
			logger.Warnf("[%s] Échec de l'arrêt du scheduler lors de la pause : %v", b.Config.ExchangeName, err)
		}
	}
	return nil
}

// Resume relance le bot après une pause : recharge et redémarre le scheduler cron
// (équivalent à un reload des stratégies) et réactive achats/ventes périodiques.
func (b *Bot) Resume() error {
	if !b.paused.Swap(false) {
		return nil // n'était pas en pause
	}

	logger.Infof("[%s] ▶️ Reprise du bot", b.Config.ExchangeName)
	return b.ReloadStrategies()
}

// IsPaused indique si le bot est actuellement en pause.
func (b *Bot) IsPaused() bool {
	return b.paused.Load()
}

// ForceBuy déclenche un achat MANUEL immédiat sur la première stratégie activée
// supportant l'achat forcé (cf. algorithms.ForceBuyer). Il court-circuite la condition
// d'entrée (RSI) et le cooldown périodique : c'est l'override opérateur du bouton
// Telegram « Acheter ». L'ordre reste un limite maker sous le marché (mêmes prix/taille
// que l'achat automatique) ; il peut donc rester en attente jusqu'à ce que le prix le
// touche. Comme l'ancre du cooldown est le dernier cycle, cet achat repousse aussi le
// prochain achat automatique. Retourne un résumé lisible pour Telegram.
func (b *Bot) ForceBuy() (string, error) {
	strategies, err := b.db.GetAllStrategies()
	if err != nil {
		return "", fmt.Errorf("lecture des stratégies : %w", err)
	}

	var target *database.Strategy
	for i := range strategies {
		if strategies[i].Enabled {
			target = &strategies[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("aucune stratégie activée")
	}

	res, err := b.strategyScheduler.GetStrategyManager().ExecuteForcedBuyStrategy(*target)
	if err != nil {
		return "", err
	}

	quote := b.market.QuoteAsset
	cost := res.Amount * res.LimitPrice
	msg := fmt.Sprintf(
		"🛒 Achat manuel posé (%s)\nMontant : %s %s @ %s %s (≈ %.2f %s)\nCible de vente : %s %s",
		target.Name,
		b.market.FormatAmount(res.Amount), b.market.GetBaseAsset(),
		b.market.FormatPrice(res.LimitPrice), quote,
		cost, quote,
		b.market.FormatPrice(res.TargetPrice), quote,
	)
	logger.Infof("[%s] %s", b.Config.ExchangeName, msg)
	return msg, nil
}

// checkErrorAlerts envoie une notification Telegram quand de nouvelles erreurs
// sont apparues depuis la dernière alerte, au plus une fois par errorAlertCooldown.
// Capture toute la catégorie « process vivant mais dysfonctionnel » (ordre refusé,
// clé API invalide…) que le heartbeat/dead-man's switch ne détecte pas.
func (b *Bot) checkErrorAlerts() {
	msg, _, count := logger.LastError()
	if count == 0 || count == b.lastAlertCount {
		return // aucune erreur, ou aucune nouvelle depuis la dernière alerte
	}
	if !b.lastAlertAt.IsZero() && time.Since(b.lastAlertAt) < errorAlertCooldown {
		return // throttle : on réessaiera après le cooldown (lastAlertCount inchangé)
	}

	b.lastAlertCount = count
	b.lastAlertAt = time.Now()

	// Alerte courte : un résumé d'une ligne, le détail complet restant consultable
	// dans la vue status (inutile de le dupliquer).
	b.emit(notify.Event{
		Kind:  notify.KindError,
		Level: notify.LevelError,
		Title: fmt.Sprintf("Le bot rencontre des erreurs (%d)", count),
		Text:  firstLine(msg, 120),
		Fields: map[string]string{
			"count": strconv.FormatInt(count, 10),
		},
	})
}

// firstLine retourne la première ligne de s, tronquée à max runes (avec « … »).
func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// pingHealthcheck notifie le service « dead-man's switch » que le bot est vivant.
// No-op si HEALTHCHECK_URL n'est pas configurée. Non bloquant, erreurs ignorées
// (l'absence de ping est justement le signal d'alerte côté service distant).
func (b *Bot) pingHealthcheck() {
	url := b.Config.HealthcheckURL
	if url == "" {
		return
	}
	go func() {
		resp, err := healthClient.Get(url)
		if err != nil {
			logger.Debugf("[%s] Ping healthcheck échoué : %v", b.Config.ExchangeName, err)
			return
		}
		_ = resp.Body.Close()
	}()
}

func (b *Bot) run() {
	logger.Infof("[%s] Starting cron-based strategy scheduler...", b.Config.ExchangeName)

	// Start the strategy scheduler with real cron
	err := b.strategyScheduler.Start()
	if err != nil {
		logger.Fatalf("Failed to start strategy scheduler: %v", err)
		return
	}

	// Strategy scheduler manages buy signals, we handle ongoing position/order management
	checkTicker := time.NewTicker(b.Config.CheckInterval)
	defer checkTicker.Stop()

	logger.Infof("[%s] Cron scheduler active, strategies will execute according to their cron expressions", b.Config.ExchangeName)

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
			b.handlePriceCheck()     // Update position max prices + trailing stop
			b.executeBuyStrategies() // Achats périodiques (stratégies sans cron)
			b.handleOrderCheck()     // Check pending orders status
			b.handleStaleBuyOrders() // Annule les ordres d'achat en attente trop vieux
			b.checkReversalSignal()  // Notif Telegram si un creux (marteau/étoile 1h) se forme
			b.ShowStatistics()
			b.checkErrorAlerts() // Alerte Telegram throttlée si nouvelles erreurs
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

	// Price-check réussi (boucle vivante + exchange joignable) : on enregistre le
	// heartbeat et on ping le dead-man's switch. Un échec plus haut saute ces deux
	// étapes — c'est précisément ce qui déclenche l'alerte distante.
	b.lastCheck.Store(time.Now().UnixNano())
	b.pingHealthcheck()

	// Get all open positions (from all strategies)
	cycles, err := b.db.GetOpenCycles()
	if err != nil {
		logger.Errorf("Failed to get open cycles: %v", err)
		return
	}

	logger.Debugf("Updating max price for %d open cycles", len(cycles))
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
	}

	logger.Debug("Check for sell signals")
	b.executeSellStrategies(currentPrice)
}

func (b *Bot) executeSellStrategies(currentPrice float64) {
	if b.paused.Load() {
		logger.Debugf("[%s] En pause : ventes suspendues", b.Config.ExchangeName)
		return
	}

	logger.Debugf("[%s] Executing sell strategies for all strategies", b.Config.ExchangeName)

	strategies, err := b.db.GetAllStrategies()
	if err != nil {
		logger.Errorf("[%s] Failed to get enabled strategies for sell execution: %v", b.Config.ExchangeName, err)
		return
	}

	strategyManager := b.strategyScheduler.GetStrategyManager()

	for _, strategy := range strategies {
		logger.Debugf("[%s] Checking sell signals for strategy: %s", b.Config.ExchangeName, strategy.Name)

		// Execute sell logic only
		err = strategyManager.ExecuteSellStrategy(strategy, currentPrice)
		if err != nil {
			logger.Errorf("[%s] Failed to execute sell strategy %s: %v", b.Config.ExchangeName, strategy.Name, err)
			continue
		}
	}
}

// executeBuyStrategies évalue les achats des stratégies en mode périodique
// (intervalle, sans cron) à chaque tick, en respectant un cooldown minimum entre
// deux achats. Tant que ShouldBuy est faux, on ré-évalue au tick suivant ; une
// fois un achat posé (cycle créé), on attend l'intervalle avant le prochain.
// Le mode cron reste piloté par le scheduler.
func (b *Bot) executeBuyStrategies() {
	if b.paused.Load() {
		logger.Debugf("[%s] En pause : achats périodiques suspendus", b.Config.ExchangeName)
		return
	}

	strategies, err := b.db.GetAllStrategies()
	if err != nil {
		logger.Errorf("[%s] Failed to get strategies for periodic buy: %v", b.Config.ExchangeName, err)
		return
	}

	strategyManager := b.strategyScheduler.GetStrategyManager()

	for _, strategy := range strategies {
		// On ne gère ici que les stratégies périodiques activées.
		if !strategy.Enabled || strategy.BuyIntervalSeconds <= 0 || strings.TrimSpace(strategy.CronExpression) != "" {
			continue
		}

		interval := time.Duration(strategy.BuyIntervalSeconds) * time.Second
		lastBuy, err := b.db.GetLastBuyTime(strategy.ID)
		if err != nil {
			logger.Errorf("[%s] Failed to get last buy time for strategy %s: %v", b.Config.ExchangeName, strategy.Name, err)
			continue
		}
		if lastBuy != nil {
			if elapsed := time.Since(*lastBuy); elapsed < interval {
				logger.Debugf("[%s] Strategy %s: cooldown actif, %s restant", b.Config.ExchangeName, strategy.Name, (interval - elapsed).Round(time.Second))
				continue
			}
		}

		logger.Infof("[%s] Executing periodic BUY strategy '%s' (intervalle %s)", b.Config.ExchangeName, strategy.Name, interval)
		if err := strategyManager.ExecuteBuyStrategy(strategy); err != nil {
			logger.Errorf("[%s] Failed to execute periodic buy strategy %s: %v", b.Config.ExchangeName, strategy.Name, err)
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

// handleStaleBuyOrders annule automatiquement les ordres d'achat en attente plus
// vieux que le seuil défini par leur stratégie (max_buy_order_age_hours). 0 = désactivé.
func (b *Bot) handleStaleBuyOrders() {
	pendingOrders, err := b.db.GetPendingOrders()
	if err != nil {
		logger.Errorf("Failed to get pending orders for stale buy sweep: %v", err)
		return
	}

	for _, dbOrder := range pendingOrders {
		if dbOrder.Side != database.Buy || dbOrder.StrategyID == nil {
			continue
		}

		strategy, err := b.db.GetStrategy(*dbOrder.StrategyID)
		if err != nil {
			logger.Errorf("[%s] Balayage achats périmés : stratégie %d introuvable : %v", b.Config.ExchangeName, *dbOrder.StrategyID, err)
			continue
		}
		if strategy.MaxBuyOrderAgeHours <= 0 {
			continue
		}

		maxAge := time.Duration(strategy.MaxBuyOrderAgeHours) * time.Hour
		age := time.Since(dbOrder.CreatedAt)
		if age < maxAge {
			continue
		}

		logger.Infof("[%s] Ordre d'achat %s (stratégie %s) en attente depuis %s (> %s) : annulation automatique",
			b.Config.ExchangeName, dbOrder.ExternalID, strategy.Name, age.Round(time.Minute), maxAge)

		if _, err := b.exchange.CancelOrder(dbOrder.ExternalID, b.Config.Pair); err != nil {
			// L'ordre a pu se remplir entre-temps : le cancel échoue, handleOrderCheck
			// (tick courant ou suivant) traitera le remplissage. On ne force rien ici.
			logger.Errorf("[%s] Échec annulation ordre d'achat périmé %s : %v", b.Config.ExchangeName, dbOrder.ExternalID, err)
			continue
		}

		// Resynchronise depuis la vérité exchange : route vers handleCanceledOrder
		// (statut CANCELLED + notif Telegram) ou handleClosedOrder en cas de course
		// fill/cancel (l'ordre s'est rempli juste avant l'annulation).
		b.processOrder(dbOrder)
	}
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

	logger.Infof("[%s] Order %v Cancelled (cancelled manually on exchange)", b.Config.ExchangeName, order.Id)
}

func (b *Bot) handleFilledBuyOrder(dbOrder database.Order, order Order) {
	dbCycle, err := b.db.GetCycleForBuyOrder(dbOrder.ID)
	if err != nil {
		logger.Errorf("Failed to get cycle from buy order %v: %v", dbOrder.ID, err)
	}

	quantity := b.market.FormatAmount(*order.Amount)
	price := b.market.FormatPrice(*order.Price)
	value := fmt.Sprintf("%.2f", *order.Amount**order.Price)

	b.emit(notify.Event{
		Kind:  notify.KindBuyFilled,
		Level: notify.LevelInfo,
		Title: fmt.Sprintf("Achat rempli — cycle #%d", dbCycle.ID),
		Text: fmt.Sprintf("%s %s @ %s %s (≈ %s %s)",
			quantity, b.market.BaseAsset, price, b.market.QuoteAsset, value, b.market.QuoteAsset),
		Fields: map[string]string{
			"cycle_id": strconv.Itoa(dbCycle.ID),
			"order_id": *order.Id,
			"quantity": quantity,
			"base":     b.market.BaseAsset,
			"price":    price,
			"quote":    b.market.QuoteAsset,
			"value":    value,
		},
	})

	logger.Infof("[%s] Buy Order Filled: %s %s at %s %s (ID=%v)",
		b.Config.ExchangeName,
		b.market.FormatAmount(*order.Amount), b.market.BaseAsset, b.market.FormatPrice(*order.Price), b.market.QuoteAsset,
		order.Id)

	// Le prix cible est défini par la stratégie
	strategy, err := b.db.GetStrategy(dbCycle.StrategyID)
	if err != nil {
		logger.Errorf("Failed to get strategy for cycle : %v", err)
		return
	}

	targetPrice := *order.Price * (1.0 + strategy.ProfitTarget/100.0)

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

	buyValue := dbCycle.BuyOrder.Price * dbCycle.BuyOrder.Amount
	win := *dbCycle.Profit
	winPercent := (win / buyValue) * 100

	quantity := b.market.FormatAmount(*order.Amount)
	price := b.market.FormatPrice(*order.Price)
	value := fmt.Sprintf("%.2f", *order.Amount**order.Price)
	profit := fmt.Sprintf("%.2f", win)
	profitPct := fmt.Sprintf("%+.1f", winPercent)

	b.emit(notify.Event{
		Kind:  notify.KindSellFilled,
		Level: notify.LevelInfo,
		Title: fmt.Sprintf("Vente remplie — cycle #%d", dbCycle.ID),
		Text:  fmt.Sprintf("Profit : %s %s (%s%%)", profit, b.market.QuoteAsset, profitPct),
		Fields: map[string]string{
			"cycle_id":   strconv.Itoa(dbCycle.ID),
			"order_id":   *order.Id,
			"quantity":   quantity,
			"base":       b.market.BaseAsset,
			"price":      price,
			"quote":      b.market.QuoteAsset,
			"value":      value,
			"profit":     profit,
			"profit_pct": profitPct,
		},
	})

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

// ReloadStrategies reloads all strategies from database
func (b *Bot) ReloadStrategies() error {
	logger.Infof("[%s] Reloading strategies...", b.Config.ExchangeName)

	if b.strategyScheduler == nil {
		return fmt.Errorf("strategy scheduler not initialized")
	}

	// Stop the current scheduler with timeout handling
	stopErr := b.strategyScheduler.Stop()
	if stopErr != nil {
		logger.Warnf("Warning: Failed to stop strategy scheduler gracefully: %v", stopErr)
		// Continue anyway - we'll try to restart
	}

	// Create a new strategy scheduler instead of restarting the old one
	strategyScheduler, err := scheduler.NewStrategyScheduler(
		b.Config.ExchangeName,
		b.Config.Pair,
		b.db,
		&b.market,
		b.marketCollector,
		b.Calculator,
		b.algorithmRegistry,
		b)
	if err != nil {
		logger.Errorf("Failed to create new strategy scheduler: %v", err)
		return err
	}

	// Replace the old scheduler with the new one
	b.strategyScheduler = strategyScheduler

	// Start the new scheduler with fresh strategies from database
	err = b.strategyScheduler.Start()
	if err != nil {
		logger.Errorf("Failed to start new strategy scheduler: %v", err)
		return err
	}

	logger.Infof("[%s] Strategies reloaded successfully", b.Config.ExchangeName)
	return nil
}

// CollectCandles collects candles using the market collector and returns count statistics
func (b *Bot) CollectCandles(pair, timeframe string, limit int) (int, int, error) {
	logger.Infof("[%s] Collecting candles for %s %s (limit: %d)", b.Config.ExchangeName, pair, timeframe, limit)

	// Get candles count before collection
	beforeCandles, err := b.db.GetCandles(pair, timeframe, 10000) // Get a large number to count
	if err != nil {
		logger.Warnf("Failed to get candles count before collection: %v", err)
	}
	beforeCount := len(beforeCandles)

	// Use the existing market collector
	err = b.marketCollector.CollectCandles(pair, timeframe, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to collect candles: %w", err)
	}

	// Get candles count after collection
	afterCandles, err := b.db.GetCandles(pair, timeframe, 10000) // Get a large number to count
	if err != nil {
		logger.Warnf("Failed to get candles count after collection: %v", err)
		// Return success with unknown saved count
		return limit, 0, nil
	}
	afterCount := len(afterCandles)

	fetched := limit                  // We requested this many
	saved := afterCount - beforeCount // This many were actually saved (new ones)

	logger.Infof("[%s] Collection completed: fetched %d, saved %d new candles for %s %s",
		b.Config.ExchangeName, fetched, saved, pair, timeframe)

	return fetched, saved, nil
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

func (m *Market) GetBaseAsset() string {
	return m.BaseAsset
}

func (m *Market) GetQuoteAsset() string {
	return m.QuoteAsset
}

func (m *Market) GetPrecision() algorithms.MarketPrecision {
	return algorithms.MarketPrecision{
		Price:          m.Precision.Price,
		PriceDecimals:  m.Precision.PriceDecimals,
		Amount:         m.Precision.Amount,
		AmountDecimals: m.Precision.AmountDecimals,
	}
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

// FetchBalances implémente api.BotInterface : retourne les soldes non nuls (free/used/total),
// la devise de base, la devise de cotation et le prix courant de la paire configurée.
func (b *Bot) FetchBalances() (map[string]api.BalanceAmounts, string, string, float64, error) {
	rawBalances, err := b.exchange.FetchBalance()
	if err != nil {
		return nil, "", "", 0, fmt.Errorf("fetch balance: %w", err)
	}

	balances := make(map[string]api.BalanceAmounts)
	for asset, bal := range rawBalances {
		if bal.Total > 0 {
			balances[asset] = api.BalanceAmounts{Free: bal.Free, Used: bal.Used, Total: bal.Total}
		}
	}

	currentPrice, err := b.exchange.GetPrice(b.market.Symbol)
	if err != nil {
		// Prix non disponible : on retourne quand même les soldes
		logger.Warnf("[%s] Could not fetch price for portfolio valuation: %v", b.ExchangeName(), err)
		currentPrice = 0
	}

	return balances, b.market.BaseAsset, b.market.QuoteAsset, currentPrice, nil
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
