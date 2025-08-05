package main

import (
	"bot/internal/trading"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ccxt/ccxt/go/v4"
)

type Position struct {
	Price     float64
	Amount    float64
	Timestamp time.Time
}

type OrderSide int

const (
	Buy OrderSide = iota
	Sell
)

type PendingOrder struct {
	Side      OrderSide // Buy or Sell
	ID        string    // ID de l'ordre
	Amount    float64   // Quantité demandée
	Price     float64   // Prix limite demandé
	Timestamp time.Time // Moment de la création de l'ordre
}

type BotState struct {
	Positions     []Position
	PendingOrders map[string]PendingOrder
}

func main() {
	log.Println("Starting Simple Bot")

	// Configurer l'échange (ex. Binance)
	exchange := ccxt.CreateExchange("mexc", map[string]interface{}{
		"apiKey":          os.Getenv("API_KEY"),
		"secret":          os.Getenv("API_SECRET"),
		"enableRateLimit": true,
	})
	if exchange == nil {
		log.Fatal("Failed to create MEXC  exchange instance")
	}

	log.Println("MEXC exchange initialized. We are trading the BTC/USDC pair in Spot Market")

	// Paramètres du bot
	pair := "BTC/USDC"
	amountUSDC := 50.0
	priceOffset := 200.0
	profitThreshold := 1.015 // 1.5% au-dessus du prix d'achat

	pendingOrders := make(map[string]PendingOrder)
	var positions []Position

	// Charger les précisions du marché pour BTC/USDC
	log.Println("Fetching market data...")
	markets, err := exchange.FetchMarkets()
	if err != nil {
		log.Fatalf("Error during fetch of market data: %v", err)
	}
	var pricePrecision, amountPrecision float64
	for _, market := range markets {
		if market.Symbol != nil && *market.Symbol == pair {
			// Extraire les précisions depuis market.Info["precision"]
			if precision, ok := market.Info["precision"].(map[string]interface{}); ok {
				if pp, ok := precision["price"].(float64); ok {
					pricePrecision = pp
				} else {
					log.Println("precision.price not found, use of default value: 0.01")
					pricePrecision = 0.01 // Valeur par défaut (2 décimales)
				}
				if ap, ok := precision["amount"].(float64); ok {
					amountPrecision = ap
				} else {
					log.Println("precision.amount not found, use of default value: 0.000001")
					amountPrecision = 0.000001 // Valeur par défaut (6 décimales)
				}
			} else {
				log.Println("precision data not found in market.Info, use of default values")
				pricePrecision = 0.01
				amountPrecision = 0.000001
			}
			break
		}
	}

	log.Printf("Got market data: pricePrecision=%v, amountPrecision=%v", pricePrecision, amountPrecision)

	if data, err := os.ReadFile("bot_state.json"); err == nil {
		var state BotState
		if err := json.Unmarshal(data, &state); err == nil {
			positions = state.Positions
			pendingOrders = state.PendingOrders
			log.Println("Bot state restored from bot_state.json")
		}
	}

	log.Printf("Starting Tickers...")

	done := make(chan bool)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		ticker := time.NewTicker(4 * time.Hour) // Planificateur pour les achats (toutes les 4 heures)
		defer ticker.Stop()
		priceCheck := time.NewTicker(5 * time.Minute) // Vérifie les prix toutes les 5 minutes
		defer priceCheck.Stop()
		orderCheck := time.NewTicker(5 * time.Minute) // Vérifie les ordres toutes les 5 minutes
		defer orderCheck.Stop()

		for {
			select {
			case <-done:
				state := BotState{
					Positions:     positions,
					PendingOrders: pendingOrders,
				}
				data, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					log.Printf("Failed to marshall bot state: %v", err)
				} else {
					if err := os.WriteFile("bot_state.json", data, 0644); err != nil {
						log.Printf("Failed to write bot state into bot_state.json : %v", err)
					} else {
						log.Println("Bot state saved to bot_state.json")
					}
				}
				return

			case <-ticker.C:
				log.Println("Time to place a new Buy Order...")

				// Vérifier le solde
				balance, err := exchange.FetchBalance(map[string]interface{}{})
				if err != nil {
					log.Printf("Failed to fetch balances: %v", err)
					continue
				}
				usdcBalance, ok := balance.Free["USDC"]
				if !ok || (*usdcBalance < amountUSDC) {
					log.Printf("USDC balance not found or insufficient: %v", usdcBalance)
					continue
				}

				log.Printf("USDC balance: %v", usdcBalance)

				// Passer un ordre d'achat
				order, err := trading.PlaceLimitBuyOrder(exchange, pair, amountUSDC, priceOffset)
				if err != nil {
					log.Printf("Failed to place Limit Buy Order: %v", err)
					continue
				}

				// Arrondir les valeurs pour respecter la précision
				orderPrice := roundToPrecision(*order.Price, pricePrecision)
				orderAmount := roundToPrecision(*order.Amount, amountPrecision)

				pendingOrders[*order.Id] = PendingOrder{
					Side:      Buy,
					ID:        *order.Id,
					Amount:    orderAmount,
					Price:     orderPrice,
					Timestamp: time.Now(),
				}
				log.Printf("Limit Buy Order placed: %v BTC at %v USDC (ID=%v)", orderAmount, orderPrice, *order.Id)

			case <-priceCheck.C:
				log.Println("Check Price...")

				// Vérifier le prix actuel
				currentPrice, err := trading.GetPrice(exchange, pair)
				if err != nil {
					log.Printf("Failed to get ticker data: %v", err)
					continue
				}

				currentPrice = roundToPrecision(currentPrice, pricePrecision)
				log.Printf("Current price: %v", currentPrice)

				// Vérifier les positions pour vendre
				for i, pos := range positions {
					if currentPrice >= pos.Price*profitThreshold {
						// Placer un ordre de vente limite à +200 USDC
						order, err := trading.PlaceLimitSellOrder(exchange, pair, pos.Amount, pos.Price, priceOffset)
						if err != nil {
							log.Printf("Failed to place Limit Sell Order: %v", err)
							continue
						}
						orderPrice := roundToPrecision(*order.Price, pricePrecision)
						orderAmount := roundToPrecision(*order.Amount, amountPrecision)

						pendingOrders[*order.Id] = PendingOrder{
							Side:      Sell,
							ID:        *order.Id,
							Amount:    orderAmount,
							Price:     orderPrice,
							Timestamp: time.Now(),
						}
						log.Printf("Limit Sell Order placed: %v BTC at %v USDC (ID=%v)", orderAmount, orderPrice, *order.Id)

						// Remove position
						positions = append(positions[:i], positions[i+1:]...)
					}
				}

			case <-orderCheck.C:
				log.Println("Check Orders...")

				// Vérifier et annuler les ordres en attente trop anciens
				for orderId, pendingOrder := range pendingOrders {
					order, err := exchange.FetchOrder(orderId)
					if err != nil {
						log.Printf("Failed to fetch Order (ID=%v): %v", orderId, err)
						continue
					}
					if *order.Status == "FILLED" {

						switch pendingOrder.Side {
						case Buy:
							log.Printf("Limit Buy Filled: %v BTC à %v USDC (ID=%v)", *order.Amount, *order.Price, orderId)

							// Ordre exécuté : ajouter à positions
							positions = append(positions, Position{
								Price:     roundToPrecision(*order.Price, pricePrecision),
								Amount:    roundToPrecision(*order.Amount, amountPrecision),
								Timestamp: time.UnixMilli(*order.Timestamp),
							})

							delete(pendingOrders, orderId)

						case Sell:
							// Ordre exécuté : retirer de positions
							log.Printf("Limit Sell Filled: %v BTC à %v USDC (ID=%v)", *order.Amount, *order.Price, orderId)
							delete(pendingOrders, orderId)
						}

					} else if order.Timestamp != nil && *order.Timestamp > 0 && time.Since(time.UnixMilli(*order.Timestamp)) > time.Hour {
						_, err := exchange.CancelOrder(orderId)
						if err != nil {
							log.Printf("Failed to Cancel Order (ID=%v): %v", orderId, err)
						} else {
							log.Printf("Order %v Cancelled (too old)", orderId)

							// TODO: Trouver comment remettre la position correspondante si on vient d'annuler un ordre "Sell"

							delete(pendingOrders, orderId)
						}
					}
				}
			}
		}
	}()

	<-sigs
	log.Println("Got a stop signal. Stopping bot...")
	close(done)
	time.Sleep(1 * time.Second)
	log.Println("Simple Bot Stopped. See Ya!")
}

// roundToPrecision arrondit une valeur à la précision spécifiée
func roundToPrecision(value, precision float64) float64 {
	factor := 1 / precision
	return float64(int64(value*factor)) / factor
}
