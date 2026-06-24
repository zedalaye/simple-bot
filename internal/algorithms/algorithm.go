package algorithms

import (
	"bot/internal/core/database"
)

// MarketPrecision represents market precision information
type MarketPrecision struct {
	Price          float64
	PriceDecimals  int
	Amount         float64
	AmountDecimals int
}

// IndicatorCalculator fournit les indicateurs techniques nécessaires aux
// algorithmes. Il est implémenté par *market.Calculator (exécution réelle,
// données live) et par le calculateur du package backtest (données historiques
// rejouées « as-of »). Garder ce contrat minimal : n'y ajouter que les méthodes
// réellement appelées par les algorithmes, pour que le backtest reste fidèle.
type IndicatorCalculator interface {
	CalculateRSI(pair, timeframe string, period int) (float64, error)
	CalculateEMA(pair, timeframe string, period int) (float64, error)
	CalculateVolatility(pair, timeframe string, period int) (float64, error)
	CalculateRecentHigh(pair, timeframe string, periods int) (float64, error)
	CalculateMACD(pair, timeframe string, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram float64, err error)
}

// TradingContext provides all necessary data for trading decisions
type TradingContext struct {
	ExchangeName string
	Pair         string
	CurrentPrice float64
	Calculator   IndicatorCalculator
	Precision    MarketPrecision // Ajout des précisions du marché
}

// Balance represents asset balance

// BuySignal represents a buy decision with pre-calculated target price
type BuySignal struct {
	ShouldBuy   bool
	Amount      float64
	LimitPrice  float64
	TargetPrice float64 // PRÉ-CALCULÉ lors de l'achat !
	Reason      string
}

// SellSignal represents a sell decision
type SellSignal struct {
	ShouldSell bool
	LimitPrice float64
	Reason     string
}

// ForceBuyer est une capacité OPTIONNELLE : un algorithme qui l'implémente peut
// produire un signal d'achat en court-circuitant ses filtres d'entrée (RSI, tendance,
// etc.). Sert aux achats manuels (ex. bouton Telegram), où l'opérateur force l'entrée.
// Les filtres de précision/taille/cible restent appliqués — seule la décision d'entrée
// est forcée. Un algorithme qui ne l'implémente pas refuse simplement l'achat manuel.
type ForceBuyer interface {
	ForceBuySignal(ctx TradingContext, strategy database.Strategy) (BuySignal, error)
}

// Algorithm defines the interface for trading algorithms
type Algorithm interface {
	// Algorithm identification
	Name() string
	Description() string

	// Trading logic
	ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error)
	ShouldSell(ctx TradingContext, cycle database.Cycle, strategy database.Strategy) (SellSignal, error)

	// Configuration validation
	ValidateConfig(strategy database.Strategy) error

	// Required indicators/parameters for this algorithm
	RequiredIndicators() []string
}

// AlgorithmRegistry manages available algorithms
type AlgorithmRegistry struct {
	algorithms map[string]Algorithm
}

// NewAlgorithmRegistry creates a new algorithm registry
func NewAlgorithmRegistry() *AlgorithmRegistry {
	registry := &AlgorithmRegistry{
		algorithms: make(map[string]Algorithm),
	}

	// Register built-in algorithms
	registry.Register(&RSI_DCA{})
	registry.Register(&MACD_Cross{})

	return registry
}

// Register adds an algorithm to the registry
func (ar *AlgorithmRegistry) Register(algorithm Algorithm) {
	ar.algorithms[algorithm.Name()] = algorithm
}

// Get retrieves an algorithm by name
func (ar *AlgorithmRegistry) Get(name string) (Algorithm, bool) {
	algorithm, exists := ar.algorithms[name]
	return algorithm, exists
}

// List returns all available algorithm names
func (ar *AlgorithmRegistry) List() []string {
	names := make([]string, 0, len(ar.algorithms))
	for name := range ar.algorithms {
		names = append(names, name)
	}
	return names
}

// GetAll returns all registered algorithms
func (ar *AlgorithmRegistry) GetAll() map[string]Algorithm {
	return ar.algorithms
}

// RoundPrice rounds a price according to market precision
func RoundPrice(price float64, precision MarketPrecision) float64 {
	factor := 1 / precision.Price
	return float64(int64(price*factor)) / factor
}

// RoundAmountUp rounds an amount UP according to market precision
// This ensures that the order value meets minimum requirements
func RoundAmount(amount float64, precision MarketPrecision) float64 {
	factor := 1 / precision.Amount
	return float64(int64(amount*factor)+1) / factor
}
