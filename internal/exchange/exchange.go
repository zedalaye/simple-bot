package exchange

import (
	"bot/internal/bot"
	"encoding/json"
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

// MexcErrorResponse représente la structure d'erreur JSON de MEXC
type MexcErrorResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// isTimestampError vérifie si l'erreur est liée à un problème de timestamp/nonce
func isTimestampError(err error) bool {
	if err == nil {
		return false
	}

	// Méthode idiomatique Go : vérifier le type d'erreur CCXT
	if ccxtError, ok := err.(*ccxt.Error); ok {
		// Vérifier si c'est une erreur InvalidNonce
		if ccxtError.Type == ccxt.InvalidNonceErrType {
			// Pour les erreurs InvalidNonce, parser le JSON pour vérifier le code spécifique
			if ccxtError.Message != "" {
				var mexcErr MexcErrorResponse
				if err := json.Unmarshal([]byte(ccxtError.Message), &mexcErr); err == nil {
					// Code 700003 = "Timestamp for this request is outside of the recvWindow"
					return mexcErr.Code == 700003
				}
			}
			// Si on ne peut pas parser le JSON mais que c'est InvalidNonce,
			// on considère que c'est probablement un problème de timestamp
			return true
		}
	}

	return false
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
// timeout, edge MEXC momentanément indisponible, throttle) qui méritent un retry. On
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
// placements d'ordre, où un « connection reset » survenu après exécution côté MEXC
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

func NewExchange(exchangeName string) *Exchange {
	var exchange ccxt.IExchange

	// On appelle les constructeurs concrets (NewMexc, NewHyperliquid) plutôt que la
	// factory générique ccxt.CreateExchange(string, ...) : celle-ci est un switch de 106
	// exchanges, tous rendus « atteignables » pour le linker (le string est runtime), ce
	// qui empêche l'élimination de code mort et embarque les 106 dans le binaire. Les
	// constructeurs typés ne joignent que l'exchange voulu (~130 Mo → ~35-45 Mo).
	switch exchangeName {
	case "mexc":
		exchange = ccxt.NewMexc(map[string]interface{}{
			"apiKey":          os.Getenv("MEXC_API_KEY"),
			"secret":          os.Getenv("MEXC_SECRET"),
			"enableRateLimit": true,
			// L'edge MEXC (Akamai) sert parfois des réponses lentes ; on laisse
			// 20 s avant de couper, plutôt que le défaut ccxt (~10 s) qui les
			// transforme en « context deadline exceeded ».
			"timeout": 20000,
		})
	case "hyperliquid":
		exchange = ccxt.NewHyperliquid(map[string]interface{}{
			"walletAddress": os.Getenv("HL_WALLET_ADDRESS"),
			"privateKey":    os.Getenv("HL_PRIVATE_KEY"),
			"options": map[string]interface{}{
				"defaultType":   "spot",
				"defaultMarket": "spot",
				"fetchMarkets": map[string]interface{}{
					"types": []string{"spot"}, // without "swap" and "hip3"
				},
			},
		})
		exchange.SetSandboxMode(os.Getenv("HL_NETWORK") == "testnet")
	}

	if exchange != nil {
		exchange.LoadMarkets()
	}

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

func (e *Exchange) PlaceLimitBuyOrder(pair string, amount float64, price float64) (bot.Order, error) {
	var result ccxt.Order
	err := retryWithBackoff(func() error {
		order, orderErr := e.CreateLimitBuyOrder(pair, amount, price)
		if orderErr == nil {
			result = order
		}
		return orderErr
	})
	if err != nil {
		return bot.Order{}, err
	}
	//fmt.Printf("DEBUG. Buy Order=%+v\n", result)
	return e.FetchOrder(*result.Id, pair)
}

func (e *Exchange) PlaceLimitSellOrder(pair string, amount float64, price float64) (bot.Order, error) {
	var result ccxt.Order
	err := retryWithBackoff(func() error {
		order, orderErr := e.CreateLimitSellOrder(pair, amount, price)
		if orderErr == nil {
			result = order
		}
		return orderErr
	})
	if err != nil {
		return bot.Order{}, err
	}
	//fmt.Printf("DEBUG. Sell Order=%+v\n", result)
	return e.FetchOrder(*result.Id, pair)
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

func withFetchMyTradeForOrderOptions(pair string, orderId string) ccxt.FetchMyTradesOptions {
	return func(opts *ccxt.FetchMyTradesOptionsStruct) {
		opts.Symbol = &pair
		opts.Params = &map[string]interface{}{}
		(*opts.Params)["orderId"] = orderId
	}
}

func (e *Exchange) FetchTradesForOrder(id string, symbol string) ([]bot.Trade, error) {
	var result []ccxt.Trade
	// e.IExchange.SetVerbose(true)
	err := retryIdempotent(func() error {
		trade, tradeErr := e.IExchange.FetchMyTrades(withFetchMyTradeForOrderOptions(symbol, id))
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

	if market.Info != nil {
		if precision, ok := market.Info["precision"].(map[string]interface{}); ok {
			if pp, ok := precision["price"].(float64); ok {
				pricePrecision = pp
				priceDecimals = -int(math.Log10(pricePrecision))
			}
			if ap, ok := precision["amount"].(float64); ok {
				amountPrecision = ap
				amountDecimals = -int(math.Log10(amountPrecision))
			}
		}

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

	var feeToken *string = nil
	if trade.Info != nil {
		if fee, ok := trade.Info["fee"].(map[string]interface{}); ok {
			if ft, ok := fee["currency"].(string); ok {
				feeToken = &ft
			}
		}
	}

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
		FeeToken:     feeToken,
	}
}
