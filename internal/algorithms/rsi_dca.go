package algorithms

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
)

// RSI_DCA implements RSI-based Dollar Cost Averaging algorithm
type RSI_DCA struct{}

// Name returns the algorithm name
func (a *RSI_DCA) Name() string {
	return "rsi_dca"
}

// Description returns the algorithm description
func (a *RSI_DCA) Description() string {
	return "RSI-based Dollar Cost Averaging with dynamic profit targets based on volatility"
}

// RequiredIndicators returns the indicators needed by this algorithm
func (a *RSI_DCA) RequiredIndicators() []string {
	return []string{"RSI", "Volatility"}
}

// ValidateConfig validates the strategy configuration for this algorithm
func (a *RSI_DCA) ValidateConfig(strategy database.Strategy) error {
	if strategy.RSIThreshold == nil {
		return fmt.Errorf("rsi_threshold is required for RSI_DCA algorithm")
	}
	if strategy.RSIPeriod == nil {
		return fmt.Errorf("rsi_period is required for RSI_DCA algorithm")
	}
	if *strategy.RSIThreshold < 0 || *strategy.RSIThreshold > 100 {
		return fmt.Errorf("rsi_threshold must be between 0 and 100, got %.2f", *strategy.RSIThreshold)
	}
	if *strategy.RSIPeriod <= 0 {
		return fmt.Errorf("rsi_period must be positive, got %d", *strategy.RSIPeriod)
	}
	if strategy.ProfitTarget <= 0 {
		return fmt.Errorf("profit_target must be positive, got %.2f", strategy.ProfitTarget)
	}
	if strategy.VolatilityPeriod != nil && *strategy.VolatilityPeriod <= 0 {
		return fmt.Errorf("volatility_period must be positive, got %d", *strategy.VolatilityPeriod)
	}

	return nil
}

// ShouldBuy determines if we should place a buy order
func (a *RSI_DCA) ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error) {
	// Validate required parameters
	if strategy.RSIThreshold == nil || strategy.RSIPeriod == nil {
		return BuySignal{}, fmt.Errorf("missing required RSI parameters for RSI_DCA algorithm")
	}

	logger.Debugf("[%s] RSI_DCA.ShouldBuy: checking RSI for %s", ctx.ExchangeName, ctx.Pair)

	// Calculate RSI using cached data
	rsi, err := ctx.Calculator.CalculateRSI(ctx.Pair, strategy.RSITimeframe, *strategy.RSIPeriod)
	if err != nil {
		return BuySignal{}, fmt.Errorf("failed to calculate RSI: %w", err)
	}

	logger.Infof("[%s] RSI_DCA.ShouldBuy: RSI = %.2f, threshold = %.2f", ctx.ExchangeName, rsi, *strategy.RSIThreshold)

	// Check RSI threshold
	if rsi > *strategy.RSIThreshold {
		return BuySignal{
			ShouldBuy: false,
			Reason:    fmt.Sprintf("RSI %.2f > threshold %.2f", rsi, *strategy.RSIThreshold),
		}, nil
	}

	// Calculate volatility for dynamic profit target
	var volatility float64 = 2.0 // Default
	if strategy.VolatilityPeriod != nil && strategy.VolatilityAdjustment != nil {
		volatility, err = ctx.Calculator.CalculateVolatility(ctx.Pair, strategy.VolatilityTimeframe, *strategy.VolatilityPeriod)
		if err != nil {
			logger.Warnf("[%s] Failed to calculate volatility, using default %.2f%%: %v", ctx.ExchangeName, volatility, err)
		}
	}

	logger.Infof("[%s] RSI_DCA.ShouldBuy: volatility = %.2f%%", ctx.ExchangeName, volatility)

	// Calculate dynamic profit target based on volatility
	volatilityFactor := (volatility - strategy.ProfitTarget) / 100.0
	adjustmentPercent := volatilityFactor * (*strategy.VolatilityAdjustment / 100.0)
	dynamicProfitPercent := (strategy.ProfitTarget / 100.0) + adjustmentPercent

	// Clamp dynamic profit over 0.1%
	if dynamicProfitPercent < 0.001 {
		dynamicProfitPercent = 0.001
	}

	// Calculate buy price with dynamic offset proportional to the RSI, the lower the RSI, the closer to the currentPrice
	dynamicOffsetPercent := -((0.1 / 100.0) + (rsi/100.0)/100.0)
	// When ProfitTarget is < 1%, we are in Scalping mode so we want to buy more closely to the current price
	if dynamicProfitPercent < 0.01 {
		baseOffsetPercent := dynamicOffsetPercent

		profitTargetFactor := dynamicProfitPercent * 100.0
		dynamicOffsetPercent = dynamicOffsetPercent * profitTargetFactor

		logger.Infof("[%s] RSI_DCA.ShouldBuy: Reducing offset due to low profit target - Factor=%.3f, OriginalOffset=%.4f%%, AdjustedOffset=%.4f%%",
			ctx.ExchangeName,
			profitTargetFactor, baseOffsetPercent*100.0, dynamicOffsetPercent*100.0)
	}
	dynamicOffset := ctx.CurrentPrice * dynamicOffsetPercent
	limitPrice := ctx.CurrentPrice + dynamicOffset

	// Round limit price according to market precision
	limitPrice = RoundPrice(limitPrice, ctx.Precision)

	// PRÉ-CALCULER le prix cible ici ! Plus besoin de le recalculer constamment
	targetPrice := limitPrice * (1.0 + dynamicProfitPercent)
	targetPrice = RoundPrice(targetPrice, ctx.Precision)

	// Calculate amount to buy and round UP according to market precision
	// This ensures the order value meets the minimum quote amount requirement
	baseAmount := strategy.QuoteAmount / limitPrice
	baseAmount = RoundAmount(baseAmount, ctx.Precision)

	logger.Infof("[%s] RSI_DCA.ShouldBuy: BUY signal - RSI=%.2f, Volatility=%.2f%%, TargetProfit=%.2f%%, TargetPrice=%.4f",
		ctx.ExchangeName, rsi, volatility, dynamicProfitPercent*100.0, targetPrice)

	return BuySignal{
		ShouldBuy:   true,
		Amount:      baseAmount,
		LimitPrice:  limitPrice,
		TargetPrice: targetPrice,
		Reason: fmt.Sprintf("RSI %.2f < %.2f, Volatility=%.2f%%, DynamicProfitTarget=%.2f%%",
			rsi, *strategy.RSIThreshold, volatility, dynamicProfitPercent*100.0),
	}, nil
}

// ShouldSell determines if we should sell a position
func (a *RSI_DCA) ShouldSell(ctx TradingContext, cycle database.Cycle, strategy database.Strategy) (SellSignal, error) {
	logger.Debugf("[%s] RSI_DCA.ShouldSell: checking cycle %d", ctx.ExchangeName, cycle.ID)

	// Check if current price has reached the target price
	if ctx.CurrentPrice >= cycle.TargetPrice {
		// Apply trailing stop logic (same as original bot.go)
		trailingStopThreshold := 1.0 - (strategy.TrailingStopDelta / 100.0)

		if ctx.CurrentPrice < (cycle.MaxPrice * trailingStopThreshold) {
			// Price has dropped from max, time to sell
			priceOffset := ctx.CurrentPrice * (strategy.SellOffset / 100.0)
			limitPrice := ctx.CurrentPrice + priceOffset

			// Round limit price according to market precision
			limitPrice = RoundPrice(limitPrice, ctx.Precision)

			logger.Infof("[%s] RSI_DCA.ShouldSell: SELL signal - Trailing Stop triggered for position in cycle %d", ctx.ExchangeName, cycle.ID)

			return SellSignal{
				ShouldSell: true,
				LimitPrice: limitPrice,
				Reason: fmt.Sprintf("Trailing Stop: %.4f < Max %.4f * %.4f%% = %.4f",
					ctx.CurrentPrice, cycle.MaxPrice, (1.0-trailingStopThreshold)*100.0, cycle.MaxPrice*trailingStopThreshold),
			}, nil
		}
	}

	// No sell signal
	return SellSignal{
		ShouldSell: false,
		Reason: fmt.Sprintf("Holding position - current %.4f, target %.4f, max %.4f",
			ctx.CurrentPrice, cycle.TargetPrice, cycle.MaxPrice),
	}, nil
}

// GetParameterHints returns hints for configuring this algorithm
func (a *RSI_DCA) GetParameterHints() map[string]string {
	return map[string]string{
		"rsi_threshold":         "RSI threshold for buy signals (30-70 typical range)",
		"rsi_period":            "RSI calculation period (14 is standard)",
		"rsi_timeframe":         "Timeframe for RSI calculation (1m, 5m, 15m, 1h, 4h, 1d)",
		"profit_target":         "Base profit target in percentage (1.0-10.0 typical)",
		"volatility_period":     "Period for volatility calculation (7 days typical)",
		"volatility_adjustment": "Volatility adjustment factor (50.0 = 50% per 1% volatility)",
		"volatility_timeframe":  "Timeframe for volatility calculation",
		"trailing_stop_delta":   "Trailing stop percentage (0.1-1.0 typical)",
		"sell_offset":           "Price offset above market for sell orders (0.1-0.5 typical)",
	}
}
