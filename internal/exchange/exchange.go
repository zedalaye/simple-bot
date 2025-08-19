package exchange

import (
	"bot/internal/bot"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ccxt/ccxt/go/v4"
)

const (
	maxRetries     = 5
	baseRetryDelay = 1000 * time.Millisecond
)

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

// retryWithBackoff exécute une fonction avec retry et backoff exponentiel
func retryWithBackoff(operation func() error) error {
	var lastError error

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil // Succès
		}

		lastError = err

		// Si ce n'est pas une erreur de timestamp, ne pas réessayer
		if !isTimestampError(err) {
			return err
		}

		// Si c'est le dernier essai, retourner l'erreur
		if attempt == maxRetries-1 {
			return err
		}

		// Calculer le délai avec backoff exponentiel + jitter
		delay := baseRetryDelay * time.Duration(1<<attempt) // 1000ms, 2000ms, 4000ms
		jitter := time.Duration(float64(delay) * 0.1)       // 10% de jitter
		totalDelay := delay + jitter

		time.Sleep(totalDelay)
	}

	return lastError
}

type Exchange struct {
	ccxt.IExchange
}

func NewExchange(exchangeName string) *Exchange {
	var exchange ccxt.IExchange

	if exchangeName == "mexc" {
		exchange = ccxt.CreateExchange("mexc", map[string]interface{}{
			"apiKey":          os.Getenv("API_KEY"),
			"secret":          os.Getenv("SECRET"),
			"enableRateLimit": true,
		})
	} else if exchangeName == "hyperliquid" {
		exchange = ccxt.CreateExchange("hyperliquid", map[string]interface{}{
			"walletAddress": os.Getenv("WALLET_ADDRESS"),
			"privateKey":    os.Getenv("PRIVATE_KEY"),
			"defaultType":   "spot",
		})
		exchange.SetSandboxMode(os.Getenv("NETWORK") == "testnet")
	}

	if exchange != nil {
		exchange.LoadMarkets()
	}

	return &Exchange{exchange}
}

func (e *Exchange) GetPrice(pair string) (float64, error) {
	var result ccxt.Ticker
	err := retryWithBackoff(func() error {
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
	return toBotOrder(result), nil
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
	return toBotOrder(result), nil
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
	err := retryWithBackoff(func() error {
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
	for key, value := range result.Free {
		botBalances[key] = bot.Balance{Free: *value}
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
	err := retryWithBackoff(func() error {
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

func (e *Exchange) FetchOrder(id string, symbol string) (bot.Order, error) {
	var result ccxt.Order
	err := retryWithBackoff(func() error {
		order, orderErr := e.IExchange.FetchOrder(id, ccxt.WithFetchOrderSymbol(symbol))
		if orderErr == nil {
			result = order
		}
		return orderErr
	})
	if err != nil {
		return bot.Order{}, err
	}
	//fmt.Printf("DEBUG. Fetched Order=%+v\n", result)
	return toBotOrder(result), nil
}

func (e *Exchange) CancelOrder(id string, symbol string) (bot.Order, error) {
	var result ccxt.Order
	err := retryWithBackoff(func() error {
		order, orderErr := e.IExchange.CancelOrder(id, ccxt.WithCancelOrderSymbol(symbol))
		if orderErr == nil {
			result = order
		}
		return orderErr
	})
	if err != nil {
		return bot.Order{}, err
	}
	return toBotOrder(result), nil
}

func toBotMarket(market ccxt.MarketInterface) bot.Market {
	pricePrecision := 0.01
	amountPrecision := 0.000001
	if market.Info != nil {
		if precision, ok := market.Info["precision"].(map[string]interface{}); ok {
			if pp, ok := precision["price"].(float64); ok {
				pricePrecision = pp
			}
			if ap, ok := precision["amount"].(float64); ok {
				amountPrecision = ap
			}
		}
	}

	return bot.Market{
		Symbol:  *market.Symbol,
		BaseId:  *market.BaseId,
		QuoteId: *market.QuoteId,
		Precision: struct {
			Price  float64
			Amount float64
		}{
			Price:  pricePrecision,
			Amount: amountPrecision,
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
