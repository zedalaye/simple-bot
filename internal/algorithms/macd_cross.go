package algorithms

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
)

// MACD_Cross implements MACD crossover algorithm
type MACD_Cross struct{}

// Name returns the algorithm name
func (a *MACD_Cross) Name() string {
	return "macd_cross"
}

// Description returns the algorithm description
func (a *MACD_Cross) Description() string {
	return "MACD Line crosses above Signal Line for buy signals"
}

// RequiredIndicators returns the indicators needed by this algorithm
func (a *MACD_Cross) RequiredIndicators() []string {
	return []string{"MACD"}
}

// ValidateConfig validates the strategy configuration for this algorithm
func (a *MACD_Cross) ValidateConfig(strategy database.Strategy) error {
	if strategy.MACDFastPeriod <= 0 {
		return fmt.Errorf("macd_fast_period must be positive, got %d", strategy.MACDFastPeriod)
	}
	if strategy.MACDSlowPeriod <= 0 {
		return fmt.Errorf("macd_slow_period must be positive, got %d", strategy.MACDSlowPeriod)
	}
	if strategy.MACDSignalPeriod <= 0 {
		return fmt.Errorf("macd_signal_period must be positive, got %d", strategy.MACDSignalPeriod)
	}
	if strategy.MACDFastPeriod >= strategy.MACDSlowPeriod {
		return fmt.Errorf("macd_fast_period (%d) must be less than macd_slow_period (%d)",
			strategy.MACDFastPeriod, strategy.MACDSlowPeriod)
	}
	if strategy.ProfitTarget <= 0 {
		return fmt.Errorf("profit_target must be positive, got %.2f", strategy.ProfitTarget)
	}

	return nil
}

// ShouldBuy determines if we should place a buy order based on MACD crossover
func (a *MACD_Cross) ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error) {
	logger.Debugf("MACD_Cross.ShouldBuy: checking MACD for %s", ctx.Pair)

	// Calculate MACD using cached data
	macd, signal, histogram, err := ctx.Calculator.CalculateMACD(ctx.Pair, strategy.MACDTimeframe,
		strategy.MACDFastPeriod, strategy.MACDSlowPeriod, strategy.MACDSignalPeriod)
	if err != nil {
		// MACD not implemented yet, return no buy signal
		logger.Debugf("MACD calculation not available yet: %v", err)
		return BuySignal{
			ShouldBuy: false,
			Reason:    "MACD calculation temporarily disabled",
		}, nil
	}

	logger.Debugf("MACD_Cross.ShouldBuy: MACD=%.4f, Signal=%.4f, Histogram=%.4f", macd, signal, histogram)

	// Buy signal: MACD line crosses above signal line (positive histogram)
	if macd > signal && histogram > 0 {
		// Simple buy at market price
		limitPrice := ctx.CurrentPrice
		targetPrice := limitPrice * (1.0 + strategy.ProfitTarget/100.0)
		baseAmount := strategy.QuoteAmount / limitPrice

		logger.Infof("MACD_Cross.ShouldBuy: BUY signal - MACD %.4f > Signal %.4f", macd, signal)

		return BuySignal{
			ShouldBuy:   true,
			Amount:      baseAmount,
			LimitPrice:  limitPrice,
			TargetPrice: targetPrice,
			Reason:      fmt.Sprintf("MACD crossover: %.4f > %.4f (histogram: %.4f)", macd, signal, histogram),
		}, nil
	}

	// No buy signal
	return BuySignal{
		ShouldBuy: false,
		Reason:    fmt.Sprintf("No MACD crossover: %.4f <= %.4f", macd, signal),
	}, nil
}

// ShouldSell determines if we should sell a position
func (a *MACD_Cross) ShouldSell(ctx TradingContext, cycle database.Cycle, strategy database.Strategy) (SellSignal, error) {
	logger.Debugf("MACD_Cross.ShouldSell: checking cycle %d", cycle.ID)

	// Simple sell logic: sell when target price is reached
	if ctx.CurrentPrice >= cycle.TargetPrice {
		// Apply trailing stop logic
		trailingStopThreshold := 1.0 - (strategy.TrailingStopDelta / 100)

		if ctx.CurrentPrice < (cycle.MaxPrice * trailingStopThreshold) {
			// Price has dropped from max, time to sell
			priceOffset := ctx.CurrentPrice * (strategy.SellOffset / 100.0)
			limitPrice := ctx.CurrentPrice + priceOffset

			logger.Infof("MACD_Cross.ShouldSell: SELL signal - target reached and trailing stop triggered")

			return SellSignal{
				ShouldSell: true,
				LimitPrice: limitPrice,
				Reason: fmt.Sprintf("Target reached + trailing stop: current %.4f < max %.4f * %.4f%%",
					ctx.CurrentPrice, cycle.MaxPrice, (1.0-trailingStopThreshold)*100),
			}, nil
		}
	}

	// No sell signal
	return SellSignal{
		ShouldSell: false,
		Reason:     fmt.Sprintf("Holding - current %.4f, target %.4f", ctx.CurrentPrice, cycle.TargetPrice),
	}, nil
}

// GetParameterHints returns hints for configuring this algorithm
func (a *MACD_Cross) GetParameterHints() map[string]string {
	return map[string]string{
		"macd_fast_period":    "MACD fast EMA period (12 is standard)",
		"macd_slow_period":    "MACD slow EMA period (26 is standard)",
		"macd_signal_period":  "MACD signal line period (9 is standard)",
		"macd_timeframe":      "Timeframe for MACD calculation (4h, 1d typical)",
		"profit_target":       "Simple profit target in percentage",
		"trailing_stop_delta": "Trailing stop percentage",
		"sell_offset":         "Price offset above market for sell orders",
	}
}
