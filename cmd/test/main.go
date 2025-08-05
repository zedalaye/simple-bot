package main

import (
	"fmt"

	"github.com/ccxt/ccxt/go/v4"
)

func main() {
	exchange := ccxt.CreateExchange("mexc", map[string]interface{}{})

	ticker, err := exchange.FetchTicker("BTC/USDC")
	if err != nil {
		fmt.Printf("Erreur : %v\n", err)
		return
	}
	fmt.Printf("Ticker: %v\n", *ticker.Last)

	markets, err := exchange.FetchMarkets()
	if err != nil {
		fmt.Printf("Erreur : %v\n", err)
	}
	//var pricePrecision, amountPrecision float64
	for _, market := range markets {
		if *market.Symbol == "BTC/USDC" {
			if precision, ok := market.Info["precision"].(map[string]interface{}); ok {
				fmt.Printf("Market precision.amount : %v\n", precision["amount"])
				fmt.Printf("Market precision.price : %v\n", precision["price"])
			}
			break
		}
	}
}
