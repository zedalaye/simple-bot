package algorithms

import (
	"bot/internal/core/database"
	"bot/internal/market"
)

// TradingContext provides all necessary data for trading decisions
type TradingContext struct {
	Pair          string
	CurrentPrice  float64
	Balance       map[string]Balance
	OpenPositions []database.Position
	Calculator    *market.Calculator
}

// Balance represents asset balance
type Balance struct {
	Free float64
}

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

// Algorithm defines the interface for trading algorithms
type Algorithm interface {
	// Algorithm identification
	Name() string
	Description() string

	// Trading logic
	ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error)
	ShouldSell(ctx TradingContext, position database.Position, strategy database.Strategy) (SellSignal, error)

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
