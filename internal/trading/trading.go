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
func PlaceLimitBuyOrder(exchange ccxt.IExchange, pair string, quoteAmount float64, priceOffset float64) (ccxt.Order, error) {
	currentPrice, err := GetPrice(exchange, pair)
	if err != nil {
		return ccxt.Order{}, err
	}

	// Calculer le prix limite : prix actuel - offset
	limitPrice := currentPrice - priceOffset
	// Calculer la quantité à acheter (quoteAmount / prix limite)
	baseAmount := quoteAmount / limitPrice

	// Version simple sans postOnly si les options posent problème
	return exchange.CreateLimitBuyOrder(pair, baseAmount, limitPrice)
}

// PlaceLimitSellOrder place un ordre de vente limite
func PlaceLimitSellOrder(exchange ccxt.IExchange, pair string, baseAmount float64, price float64, priceOffset float64) (ccxt.Order, error) {
	// Prix limite pour la vente : prix d'achat + offset
	limitPrice := price + priceOffset

	// Version simple sans postOnly
	return exchange.CreateLimitSellOrder(pair, baseAmount, limitPrice)
}
