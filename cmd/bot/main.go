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

type PendingOrder struct {
	ID        string    // ID de l'ordre
	Amount    float64   // Quantité demandée
	Price     float64   // Prix limite demandé
	Timestamp time.Time // Moment de la création de l'ordre
}

type State struct {
	Positions        []Position
	PendingBuyOrders map[string]PendingOrder
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
		log.Fatal("Failed to create exchange")
	}

	log.Println("MEXC exchange initialized")

	// Paramètres du bot
	pair := "BTC/USDC"
	amountUSDC := 50.0
	priceOffset := 200.0
	profitThreshold := 1.015 // 1.5% au-dessus du prix d'achat

	pendingBuyOrders := make(map[string]PendingOrder)
	var positions []Position

	// Charger les précisions du marché pour BTC/USDC
	log.Println("Fetch market data...")
	markets, err := exchange.FetchMarkets()
	if err != nil {
		log.Fatal("Erreur lors de la récupération des marchés : %v", err)
	}
	var pricePrecision, amountPrecision float64
	for _, market := range markets {
		if market.Symbol != nil && *market.Symbol == pair {
			// Extraire les précisions depuis market.Info["precision"]
			if precision, ok := market.Info["precision"].(map[string]interface{}); ok {
				if pp, ok := precision["price"].(float64); ok {
					pricePrecision = pp
				} else {
					log.Println("precision.price non trouvé, utilisation de la valeur par défaut : 0.01")
					pricePrecision = 0.01 // Valeur par défaut (2 décimales)
				}
				if ap, ok := precision["amount"].(float64); ok {
					amountPrecision = ap
				} else {
					log.Println("precision.amount non trouvé, utilisation de la valeur par défaut : 0.000001")
					amountPrecision = 0.000001 // Valeur par défaut (6 décimales)
				}
			} else {
				log.Println("precision non trouvé dans market.Info, utilisation des valeurs par défaut")
				pricePrecision = 0.01
				amountPrecision = 0.000001
			}
			break
		}
	}

	log.Println("Got market data")

	if data, err := os.ReadFile("bot_state.json"); err == nil {
		var state State
		if err := json.Unmarshal(data, &state); err == nil {
			positions = state.Positions
			pendingBuyOrders = state.PendingBuyOrders
			log.Println("État du bot restauré depuis bot_state.json")
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
				state := State{
					Positions:        positions,
					PendingBuyOrders: pendingBuyOrders,
				}
				data, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					log.Printf("Erreur lors de la sauvegarde de l'état : %v", err)
				} else {
					if err := os.WriteFile("bot_state.json", data, 0644); err != nil {
						log.Printf("Erreur lors de l'écriture du fichier bot_state.json : %v", err)
					} else {
						log.Println("État du bot sauvegardé dans bot_state.json")
					}
				}
				return

			case <-ticker.C:
				log.Println("Plan a new BuyOrder...")

				// Vérifier le solde
				balance, err := exchange.FetchBalance(map[string]interface{}{})
				if err != nil {
					log.Printf("Erreur lors de la récupération du solde : %v", err)
					continue
				}
				usdcBalance, ok := balance.Free["USDC"]
				if !ok || (*usdcBalance < amountUSDC) {
					log.Printf("Solde USDC insuffisant ou non trouvé : %v", usdcBalance)
					continue
				}

				// Passer un ordre d'achat
				order, err := trading.PlaceLimitBuyOrder(exchange, pair, amountUSDC, priceOffset)
				if err != nil {
					log.Printf("Erreur lors de l'achat : %v", err)
					continue
				}

				// Arrondir les valeurs pour respecter la précision
				orderPrice := roundToPrecision(*order.Price, pricePrecision)
				orderAmount := roundToPrecision(*order.Amount, amountPrecision)

				pendingBuyOrders[*order.Id] = PendingOrder{
					ID:        *order.Id,
					Amount:    orderAmount,
					Price:     orderPrice,
					Timestamp: time.Now(),
				}
				log.Printf("Ordre d'achat limite placé : %v BTC à %v USDC (ID: %v)", orderAmount, orderPrice, *order.Id)

			case <-priceCheck.C:
				log.Println("Check Price...")

				// Vérifier le prix actuel
				currentPrice, err := trading.GetPrice(exchange, pair)
				if err != nil {
					log.Printf("Erreur lors de la récupération du ticker : %v", err)
					continue
				}
				currentPrice = roundToPrecision(currentPrice, pricePrecision)

				// Vérifier les positions pour vendre
				for i, pos := range positions {
					if currentPrice >= pos.Price*profitThreshold {
						// Placer un ordre de vente limite à +200 USDC
						order, err := trading.PlaceLimitSellOrder(exchange, pair, pos.Amount, pos.Price, priceOffset)
						if err != nil {
							log.Printf("Erreur lors de la vente : %v", err)
							continue
						}
						orderPrice := roundToPrecision(*order.Price, pricePrecision)
						orderAmount := roundToPrecision(*order.Amount, amountPrecision)
						if *order.Status == "closed" {
							log.Printf("Vente limite effectuée : %v BTC à %v USDC", orderAmount, orderPrice)
							// Supprimer la position vendue
							positions = append(positions[:i], positions[i+1:]...)
						} else {
							log.Printf("Ordre de vente limite en attente : %v BTC à %v USDC", orderAmount, orderPrice)
						}
					}
				}

			case <-orderCheck.C:
				log.Println("Check Orders...")

				// Vérifier et annuler les ordres en attente trop anciens
				for orderId, _ := range pendingBuyOrders {
					order, err := exchange.FetchOrder(orderId)
					if err != nil {
						log.Printf("Erreur lors de la récupération de l'ordre %v : %v", orderId, err)
						continue
					}
					if *order.Status == "closed" {
						// Ordre exécuté : ajouter à positions
						positions = append(positions, Position{
							Price:     roundToPrecision(*order.Price, pricePrecision),
							Amount:    roundToPrecision(*order.Amount, amountPrecision),
							Timestamp: time.UnixMilli(*order.Timestamp),
						})
						log.Printf("Achat limite exécuté : %v BTC à %v USDC (ID: %v)", *order.Amount, *order.Price, orderId)
						delete(pendingBuyOrders, orderId)
					} else if order.Timestamp != nil && *order.Timestamp > 0 && time.Since(time.UnixMilli(*order.Timestamp)) > time.Hour {
						_, err := exchange.CancelOrder(orderId)
						if err != nil {
							log.Printf("Erreur lors de l'annulation de l'ordre %v : %v", orderId, err)
						} else {
							log.Printf("Ordre %v annulé (trop ancien)", orderId)
							delete(pendingBuyOrders, orderId)
						}
					}
				}
			}
		}
	}()

	<-sigs
	log.Println("Signal d'arrêt reçu. Arrêt du bot...")
	close(done)
	time.Sleep(1 * time.Second)
	log.Println("Bot arrêté")
}

// roundToPrecision arrondit une valeur à la précision spécifiée
func roundToPrecision(value, precision float64) float64 {
	factor := 1 / precision
	return float64(int64(value*factor)) / factor
}
