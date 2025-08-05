package trading

import (
	"github.com/ccxt/ccxt/go/v4"
)

// GetPrice récupère le prix actuel d'une paire
func GetPrice(exchange ccxt.IExchange, pair string) (float64, error) {
	ticker, err := exchange.FetchTicker(pair)
	if err != nil {
		return 0, err
	}
	return *ticker.Last, nil
}

// PlaceLimitBuyOrder place un ordre d'achat limite
func PlaceLimitBuyOrder(exchange ccxt.IExchange, pair string, amountUSDC float64, priceOffset float64) (ccxt.Order, error) {
	currentPrice, err := GetPrice(exchange, pair)
	if err != nil {
		return ccxt.Order{}, err
	}
	// Calculer le prix limite : prix actuel - 200 USDC
	limitPrice := currentPrice - priceOffset
	// Calculer la quantité de BTC (amountUSDC / prix limite)
	amountBTC := amountUSDC / limitPrice

	postOnly := ccxt.CreateLimitBuyOrderOptions(func(opts *ccxt.CreateLimitBuyOrderOptionsStruct) {
		*opts.Params = map[string]interface{}{
			"postOnly": true,
		}
	})
	return exchange.CreateLimitBuyOrder(pair, amountBTC, limitPrice, postOnly)
}

// PlaceLimitSellOrder place un ordre de vente limite
func PlaceLimitSellOrder(exchange ccxt.IExchange, pair string, amountBTC float64, price float64, priceOffset float64) (ccxt.Order, error) {
	// Prix limite pour la vente : prix d'achat + 200 USDC
	limitPrice := price + priceOffset

	postOnly := ccxt.CreateLimitSellOrderOptions(func(opts *ccxt.CreateLimitSellOrderOptionsStruct) {
		*opts.Params = map[string]interface{}{
			"postOnly": true,
		}
	})

	return exchange.CreateLimitSellOrder(pair, amountBTC, limitPrice, postOnly)
}
