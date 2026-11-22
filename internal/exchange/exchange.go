package exchange

import (
	"bot/internal/bot"
	"bot/internal/logger"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	ccxt "github.com/ccxt/ccxt/go/v4"
)

const maxRetries = 5

// baseRetryDelay est une var (et non une const) pour que les tests puissent la
// réduire et éviter de dormir réellement pendant plusieurs secondes.
var baseRetryDelay = 1000 * time.Millisecond

// Timeframes supportés universellement
var SupportedTimeframes = []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d", "1w", "1M"}

// isTimestampError vérifie si l'erreur est liée à un problème de timestamp/nonce
// (horloge locale décalée). ccxt range le code OKX 50102 « Timestamp request
// expired » dans InvalidNonce : la requête a été rejetée sans effet, on peut rejouer.
func isTimestampError(err error) bool {
	ccxtError, ok := err.(*ccxt.Error)
	return ok && ccxtError.Type == ccxt.InvalidNonceErrType
}

// callSafe exécute fn en capturant les panics CCXT et en les convertissant en erreurs Go
func callSafe(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("panic: %v", r)
			}
		}
	}()
	return fn()
}

// cleanCCXTError retourne une erreur Go standard sans les stack traces internes de CCXT.
// On utilise fmt.Errorf plutôt que *ccxt.Error car Error() de ce type ajoute toujours
// "\nStack:\n..." même quand le champ est vide. L'appel à isTimestampError a déjà eu
// lieu avant cleanCCXTError dans retryWithBackoff, donc perdre le type *ccxt.Error ici est safe.
func cleanCCXTError(err error) error {
	if err == nil {
		return nil
	}
	if ccxtErr, ok := err.(*ccxt.Error); ok {
		return fmt.Errorf("[%s] %s", ccxtErr.Type, ccxtErr.Message)
	}
	// Erreur non-CCXT : tronquer à la première ligne pour supprimer d'éventuelles stack traces
	msg := err.Error()
	if idx := strings.IndexByte(msg, '\n'); idx != -1 {
		return fmt.Errorf("%s", msg[:idx])
	}
	return err
}

// transientNetworkSignatures : signatures brutes d'erreurs réseau passagères, au
// cas où l'erreur ne serait pas typée *ccxt.Error (remontée directe du transport HTTP).
var transientNetworkSignatures = []string{
	"connection reset by peer",
	"context deadline exceeded",
	"Client.Timeout exceeded",
	"connection refused",
	"i/o timeout",
	"TLS handshake timeout",
	"no such host",
	"EOF",
}

// isTransientNetworkError repère les erreurs réseau passagères (connexion coupée,
// timeout, API momentanément indisponible, throttle) qui méritent un retry. On
// les distingue des erreurs métier (fonds insuffisants, ordre invalide…) qui, elles,
// ne se résoudront pas en réessayant.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if ccxtErr, ok := err.(*ccxt.Error); ok {
		switch ccxtErr.Type {
		case ccxt.NetworkErrorErrType, ccxt.RequestTimeoutErrType,
			ccxt.ExchangeNotAvailableErrType, ccxt.DDoSProtectionErrType,
			ccxt.RateLimitExceededErrType:
			return true
		}
	}
	msg := err.Error()
	for _, sig := range transientNetworkSignatures {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// retry exécute operation avec backoff exponentiel. Les erreurs de timestamp sont
// toujours réessayées. Les erreurs réseau transitoires ne le sont que si retryNetwork
// est vrai : on l'active pour les appels idempotents (lectures), mais PAS pour les
// placements d'ordre, où un « connection reset » survenu après exécution côté exchange
// ferait passer un retry pour un doublon d'ordre.
func retry(operation func() error, retryNetwork bool) error {
	var lastError error

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := callSafe(operation)
		if err == nil {
			return nil // Succès
		}

		lastError = err

		retryable := isTimestampError(err) || (retryNetwork && isTransientNetworkError(err))
		if !retryable {
			return cleanCCXTError(err)
		}

		// Si c'est le dernier essai, retourner l'erreur
		if attempt == maxRetries-1 {
			return cleanCCXTError(err)
		}

		// Calculer le délai avec backoff exponentiel + jitter
		delay := baseRetryDelay * time.Duration(1<<attempt) // 1000ms, 2000ms, 4000ms
		jitter := time.Duration(float64(delay) * 0.1)       // 10% de jitter
		totalDelay := delay + jitter

		time.Sleep(totalDelay)
	}

	return cleanCCXTError(lastError)
}

// retryWithBackoff : chemin conservateur (timestamp uniquement), pour les opérations
// qui modifient l'état (placement/annulation d'ordre) où rejouer un échec réseau
// ambigu risquerait de dupliquer l'action.
func retryWithBackoff(operation func() error) error {
	return retry(operation, false)
}

// retryIdempotent : pour les appels en lecture seule (prix, solde, bougies, ordres,
// trades). Sûrs à rejouer, donc on absorbe aussi les blips réseau transitoires.
func retryIdempotent(operation func() error) error {
	return retry(operation, true)
}

type Exchange struct {
	ccxt.IExchange
	name string
}

// NewExchange instancie l'exchange demandé. Retourne nil si l'exchange n'est pas
// supporté : seul « okx » l'est (marché spot, compte européen).
func NewExchange(exchangeName string) *Exchange {
	var exchange ccxt.IExchange

	// On appelle les constructeurs concrets (NewMyokx) plutôt que la factory générique
	// ccxt.CreateExchange(string, ...) : celle-ci est un switch de 106 exchanges, tous
	// rendus « atteignables » pour le linker (le string est runtime), ce qui empêche
	// l'élimination de code mort et embarque les 106 dans le binaire. Les constructeurs
	// typés ne joignent que l'exchange voulu (~130 Mo → ~35-45 Mo).
	switch exchangeName {
	case "okx":
		// MyOKX = OKX Europe (entité MiCA) : même API que OKX, servie par eea.okx.com.
		// Les comptes européens (my.okx.com) n'existent que là : une clé API créée
		// sur my.okx.com est refusée par l'API globale www.okx.com.
		exchange = ccxt.NewMyokx(map[string]interface{}{
			"apiKey": os.Getenv("OKX_API_KEY"),
			"secret": os.Getenv("OKX_SECRET"),
			// Passphrase choisie à la création de la clé API (spécifique à OKX)
			"password":        os.Getenv("OKX_PASSPHRASE"),
			"enableRateLimit": true,
			"options": map[string]interface{}{
				"defaultType": "spot",
				// Spot uniquement : inutile de charger les swaps au démarrage
				"fetchMarkets": map[string]interface{}{
					"types": []interface{}{"spot"},
				},
			},
		})
		// Mode « demo trading » d'OKX (argent fictif) : nécessite des clés API
		// créées depuis l'interface de démo.
		exchange.SetSandboxMode(os.Getenv("OKX_DEMO") == "true")
	default:
		return nil
	}

	exchange.LoadMarkets()

	return &Exchange{
		IExchange: exchange,
		name:      exchangeName,
	}
}

func (e *Exchange) GetPrice(pair string) (float64, error) {
	var result ccxt.Ticker
	err := retryIdempotent(func() error {
		ticker, tickerErr := e.FetchTicker(pair)
		if tickerErr == nil {
			result = ticker
		}
		return tickerErr
	})
	if err != nil {
		return 0, err
	}
	return *result.Last, nil
}

// postOnlyParams force les ordres limite en « post-only » : ils ne peuvent
// s'exécuter qu'en maker (frais réduits). Un ordre qui croiserait immédiatement le
// carnet est annulé par l'exchange au lieu d'être exécuté en taker ; le bot le voit
// passer « canceled » au check suivant (achat abandonné, ou vente re-tentée au
// prochain signal puisque le cycle redevient éligible).
func postOnlyParams() map[string]interface{} {
	return map[string]interface{}{"postOnly": true}
}

// FetchMakerFee renvoie le taux de frais maker réel du compte pour la paire
// (fraction : 0.0008 = 0,08 %), qui dépend du palier VIP.
func (e *Exchange) FetchMakerFee(pair string) (float64, error) {
	var result ccxt.TradingFeeInterface
	err := retryIdempotent(func() error {
		fee, feeErr := e.IExchange.FetchTradingFee(pair)
		if feeErr == nil {
			result = fee
		}
		return feeErr
	})
	if err != nil {
		return 0, err
	}
	if result.Maker == nil {
		return 0, fmt.Errorf("frais maker absents de la réponse pour %s", pair)
	}
	return *result.Maker, nil
}

func (e *Exchange) PlaceLimitBuyOrder(pair string, amount float64, price float64) (bot.Order, error) {
	var result ccxt.Order
	err := retryWithBackoff(func() error {
		order, orderErr := e.CreateLimitBuyOrder(pair, amount, price,
			ccxt.WithCreateLimitBuyOrderParams(postOnlyParams()))
		if orderErr == nil {
			result = order
		}
		return orderErr
	})
	if err != nil {
		return bot.Order{}, err
	}
	//fmt.Printf("DEBUG. Buy Order=%+v\n", result)
	return e.confirmPlacedOrder(result, pair, "buy", amount, price)
}

func (e *Exchange) PlaceLimitSellOrder(pair string, amount float64, price float64) (bot.Order, error) {
	var result ccxt.Order
	err := retryWithBackoff(func() error {
		order, orderErr := e.CreateLimitSellOrder(pair, amount, price,
			ccxt.WithCreateLimitSellOrderParams(postOnlyParams()))
		if orderErr == nil {
			result = order
		}
		return orderErr
	})
	if err != nil {
		return bot.Order{}, err
	}
	//fmt.Printf("DEBUG. Sell Order=%+v\n", result)
	return e.confirmPlacedOrder(result, pair, "sell", amount, price)
}

// placedOrderFetchDelays : attentes successives avant chaque relecture d'un ordre
// qu'on vient de poser (var pour que les tests puissent les raccourcir).
var placedOrderFetchDelays = []time.Duration{
	300 * time.Millisecond, 700 * time.Millisecond, 1500 * time.Millisecond, 3 * time.Second,
}

// confirmPlacedOrder relit un ordre que l'exchange vient d'ACCEPTER. Une fois l'id
// obtenu, l'ordre vit sur l'exchange : on ne doit plus jamais renvoyer d'erreur, sinon
// l'appelant le croit non posé, ne l'enregistre pas, et le rejoue au signal suivant.
// Vécu le 2026-11-22 : OKX a répondu 51603 « Order does not exist » à la relecture
// immédiate (l'ordre n'était pas encore visible en lecture) → vente orpheline exécutée
// puis seconde vente du même cycle, prise sur la réserve de BTC.
func (e *Exchange) confirmPlacedOrder(created ccxt.Order, pair, side string, amount, price float64) (bot.Order, error) {
	if created.Id == nil {
		return bot.Order{}, fmt.Errorf("ordre %s accepté par l'exchange mais sans id", side)
	}
	id := *created.Id
	return confirmOrder(id, func() (bot.Order, error) { return e.FetchOrder(id, pair) },
		amount, price, placedOrderFetchDelays, func(err error) {
			logger.Warnf("[%s] Ordre %s %s posé mais illisible pour l'instant (%v) : enregistré tel qu'envoyé, le suivi des ordres prendra le relais",
				e.name, side, id, err)
		})
}

// confirmOrder : logique pure de confirmPlacedOrder (testable sans exchange). Tente
// fetch après chaque délai ; si l'ordre reste introuvable, renvoie l'ordre reconstruit
// à partir de la requête, statut « open ».
func confirmOrder(id string, fetch func() (bot.Order, error), amount, price float64,
	delays []time.Duration, onGiveUp func(error)) (bot.Order, error) {
	var lastErr error
	for _, d := range delays {
		time.Sleep(d)
		order, err := fetch()
		if err == nil && order.Id != nil {
			return order, nil
		}
		lastErr = err
	}
	if onGiveUp != nil {
		onGiveUp(lastErr)
	}
	status := "open"
	return bot.Order{Id: &id, Amount: &amount, Price: &price, Status: &status}, nil
}

func (e *Exchange) GetMarket(pair string) bot.Market {
	market := e.IExchange.GetMarket(pair)
	return toBotMarket(market)
}

func (e *Exchange) GetMarketsList() []bot.Market {
	markets := e.IExchange.GetMarketsList()
	result := make([]bot.Market, len(markets))
	for i, market := range markets {
		result[i] = toBotMarket(market)
	}
	return result
}

func (e *Exchange) FetchBalance() (map[string]bot.Balance, error) {
	var result ccxt.Balances
	err := retryIdempotent(func() error {
		balances, balanceErr := e.IExchange.FetchBalance()
		if balanceErr == nil {
			result = balances
		}
		return balanceErr
	})
	if err != nil {
		return nil, err
	}

	botBalances := make(map[string]bot.Balance)
	for key, total := range result.Total {
		bal := bot.Balance{Total: *total}
		if free, ok := result.Free[key]; ok && free != nil {
			bal.Free = *free
		}
		if used, ok := result.Used[key]; ok && used != nil {
			bal.Used = *used
		}
		botBalances[key] = bal
	}
	return botBalances, nil
}

func withFetchOHLCVOptions(timeframe string, since *int64, limit int64) ccxt.FetchOHLCVOptions {
	return func(opts *ccxt.FetchOHLCVOptionsStruct) {
		opts.Timeframe = &timeframe
		if since != nil {
			opts.Since = since
		}
		opts.Limit = &limit
	}
}

func (e *Exchange) FetchCandles(pair string, timeframe string, since *int64, limit int64) ([]bot.Candle, error) {
	var result []ccxt.OHLCV
	err := retryIdempotent(func() error {
		ohlcv, ohlcvErr := e.IExchange.FetchOHLCV(pair,
			withFetchOHLCVOptions(timeframe, since, limit),
		)
		if ohlcvErr == nil {
			result = ohlcv
		}
		return ohlcvErr
	})
	if err != nil {
		return nil, err
	}

	botCandles := make([]bot.Candle, len(result))
	for i, ohlcv := range result {
		botCandles[i] = toBotCandle(ohlcv)
	}
	return botCandles, nil
}

func withFetchTradeOptions(since *int64, until *int64, limit int64) ccxt.FetchTradesOptions {
	return func(opts *ccxt.FetchTradesOptionsStruct) {
		if since != nil {
			opts.Since = since
		}
		opts.Limit = &limit
		if until != nil {
			opts.Params = &map[string]interface{}{}
			(*opts.Params)["until"] = *until
		}
	}
}

func withFetchMyTradeOptions(pair string, since *int64, until *int64, limit int64) ccxt.FetchMyTradesOptions {
	return func(opts *ccxt.FetchMyTradesOptionsStruct) {
		opts.Symbol = &pair
		if since != nil {
			opts.Since = since
		}
		if until != nil {
			opts.Params = &map[string]interface{}{}
			(*opts.Params)["until"] = *until
		}
		opts.Limit = &limit
	}
}

func (e *Exchange) FetchMyTrades(pair string, since *int64, until *int64, limit int64) ([]bot.Trade, error) {
	var result []ccxt.Trade
	// e.IExchange.SetVerbose(true)
	err := retryIdempotent(func() error {
		trades, tradeErr := e.IExchange.FetchMyTrades(withFetchMyTradeOptions(pair, since, until, limit))
		if tradeErr == nil {
			result = trades
		}
		return tradeErr
	})
	// e.IExchange.SetVerbose(false)
	if err != nil {
		return nil, err
	}

	botTrades := make([]bot.Trade, len(result))
	for i, trade := range result {
		//fmt.Printf("DEBUG. Trade=%+v\n", trade)
		botTrades[i] = toBotTrade(trade)
	}
	return botTrades, nil
}

func (e *Exchange) FetchOrder(id string, symbol string) (bot.Order, error) {
	var result ccxt.Order
	// e.IExchange.SetVerbose(true)
	err := retryIdempotent(func() error {
		order, orderErr := e.IExchange.FetchOrder(id, ccxt.WithFetchOrderSymbol(symbol))
		if orderErr == nil {
			result = order
		}
		return orderErr
	})
	// e.IExchange.SetVerbose(false)
	if err != nil {
		return bot.Order{}, err
	}
	// fmt.Printf("DEBUG. Fetched Order=%+v\n", result)
	return toBotOrder(result), nil
}

func (e *Exchange) FetchTradesForOrder(id string, symbol string) ([]bot.Trade, error) {
	var result []ccxt.Trade
	// e.IExchange.SetVerbose(true)
	err := retryIdempotent(func() error {
		trade, tradeErr := e.IExchange.FetchOrderTrades(id, ccxt.WithFetchOrderTradesSymbol(symbol))
		if tradeErr == nil {
			result = trade
		}
		return tradeErr
	})
	// e.IExchange.SetVerbose(false)
	if err != nil {
		return nil, err
	}

	botTrades := make([]bot.Trade, len(result))
	for i, trade := range result {
		//fmt.Printf("DEBUG. Trade=%+v\n", trade)
		botTrades[i] = toBotTrade(trade)
	}
	return botTrades, nil
}

func (e *Exchange) CancelOrder(id string, symbol string) (bot.Order, error) {
	//var result ccxt.Order
	err := retryWithBackoff(func() error {
		_, orderErr := e.IExchange.CancelOrder(id, ccxt.WithCancelOrderSymbol(symbol))
		//if orderErr == nil {
		//	result = order
		//}
		return orderErr
	})
	if err != nil {
		return bot.Order{}, err
	}
	return e.FetchOrder(id, symbol)
	//return toBotOrder(result), nil
}

func toBotMarket(market ccxt.MarketInterface) bot.Market {
	pricePrecision := 0.01
	priceDecimals := 2

	amountPrecision := 0.000001
	amountDecimals := 6

	// Depuis ccxt 4.5.9x, Info est typé `any` (payload brut de l'exchange)
	// Précision unifiée ccxt. OKX est en mode TICK_SIZE : les valeurs sont
	// directement les pas de cotation (tickSz) et de quantité (lotSz).
	if pp := market.Precision.Price; pp != nil && *pp > 0 {
		pricePrecision = *pp
		priceDecimals = decimalsOf(pricePrecision)
	}
	if ap := market.Precision.Amount; ap != nil && *ap > 0 {
		amountPrecision = *ap
		amountDecimals = decimalsOf(amountPrecision)
	}

	return bot.Market{
		Symbol:     *market.Symbol,
		BaseId:     *market.BaseId,
		BaseAsset:  *market.BaseCurrency,
		QuoteId:    *market.QuoteId,
		QuoteAsset: *market.QuoteCurrency,
		Precision: struct {
			Price          float64
			PriceDecimals  int
			Amount         float64
			AmountDecimals int
		}{
			Price:          pricePrecision,
			PriceDecimals:  priceDecimals,
			Amount:         amountPrecision,
			AmountDecimals: amountDecimals,
		},
	}
}

// decimalsOf : nombre de décimales d'un pas (0.1 → 1, 0.00000001 → 8, 1 → 0).
func decimalsOf(step float64) int {
	d := int(math.Ceil(-math.Log10(step) - 1e-9))
	if d < 0 {
		return 0
	}
	return d
}

func toBotOrder(order ccxt.Order) bot.Order {
	return bot.Order{
		Id:        order.Id,
		Price:     order.Price,
		Amount:    order.Amount,
		Status:    order.Status,
		Timestamp: order.Timestamp,
	}
}

func toBotCandle(ohlcv ccxt.OHLCV) bot.Candle {
	return bot.Candle{
		Timestamp: ohlcv.Timestamp,
		Open:      ohlcv.Open,
		High:      ohlcv.High,
		Low:       ohlcv.Low,
		Close:     ohlcv.Close,
		Volume:    ohlcv.Volume,
	}
}

func toBotTrade(trade ccxt.Trade) bot.Trade {
	return bot.Trade{
		Id:           trade.Id,
		Timestamp:    trade.Timestamp,
		Symbol:       trade.Symbol,
		OrderId:      trade.Order,
		Type:         trade.Type,
		Side:         trade.Side,
		TakerOrMaker: trade.TakerOrMaker,
		Price:        trade.Price,
		Amount:       trade.Amount,
		Cost:         trade.Cost,
		Fee:          trade.Fee.Cost,
		FeeToken:     trade.Fee.Currency,
	}
}
