# Architecture Finale - Avec Librairie d'Indicateurs Techniques

## 🎯 Intégration de github.com/cinar/indicator

### Avantages de la Librairie
- ✅ **Fiabilité** : Implémentations testées et validées
- ✅ **Complétude** : RSI, MACD, Bollinger Bands, SMA, EMA, Stochastic, etc.
- ✅ **Performance** : Calculs optimisés
- ✅ **Maintenance** : Plus besoin de maintenir nos propres calculs
- ✅ **Extensibilité** : 50+ indicateurs disponibles pour futures stratégies

### Documentation de la Librairie
```go
// Indicateurs supportés par github.com/cinar/indicator
- RSI (Relative Strength Index)
- MACD (Moving Average Convergence Divergence)  
- Bollinger Bands
- Moving Averages (SMA, EMA, WMA, etc.)
- Stochastic Oscillator
- Williams %R
- ADX (Average Directional Index)
- CCI (Commodity Channel Index)
- Et 40+ autres...
```

## 🏗️ Architecture Mise à Jour

### Dependencies
```bash
go get github.com/cinar/indicator
```

### Service MarketData Refactorisé
```go
// internal/market/calculator.go
package market

import (
    "github.com/cinar/indicator"
    "bot/internal/core/database"
)

type Calculator struct {
    db *database.DB
}

func NewCalculator(db *database.DB) *Calculator {
    return &Calculator{db: db}
}

// RSI utilisant la librairie indicator
func (c *Calculator) CalculateRSI(pair, timeframe string, period int) (float64, error) {
    // 1. Récupérer les bougies depuis la DB
    candles, err := c.db.GetCandles(pair, timeframe, period*2) // Plus de données pour la précision
    if err != nil {
        return 0, fmt.Errorf("failed to get candles for RSI: %w", err)
    }
    
    if len(candles) < period {
        return 0, fmt.Errorf("not enough candles for RSI calculation: need %d, got %d", period, len(candles))
    }
    
    // 2. Convertir en format attendu par la librairie
    closes := make([]float64, len(candles))
    for i, candle := range candles {
        closes[i] = candle.ClosePrice
    }
    
    // 3. Calculer RSI avec la librairie (much cleaner!)
    rsiValues := indicator.Rsi(period, closes)
    
    // 4. Retourner la dernière valeur
    if len(rsiValues) == 0 {
        return 0, fmt.Errorf("RSI calculation returned no values")
    }
    
    return rsiValues[len(rsiValues)-1], nil
}

// MACD pour futures stratégies
func (c *Calculator) CalculateMACD(pair, timeframe string, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram float64, err error) {
    candles, err := c.db.GetCandles(pair, timeframe, slowPeriod*3)
    if err != nil {
        return 0, 0, 0, fmt.Errorf("failed to get candles for MACD: %w", err)
    }
    
    closes := make([]float64, len(candles))
    for i, candle := range candles {
        closes[i] = candle.ClosePrice
    }
    
    macdLine, signalLine, histogram := indicator.Macd(closes)
    
    if len(macdLine) == 0 {
        return 0, 0, 0, fmt.Errorf("MACD calculation returned no values")
    }
    
    lastIdx := len(macdLine) - 1
    return macdLine[lastIdx], signalLine[lastIdx], histogram[lastIdx], nil
}

// Bollinger Bands pour futures stratégies
func (c *Calculator) CalculateBollingerBands(pair, timeframe string, period int, k float64) (upper, middle, lower float64, err error) {
    candles, err := c.db.GetCandles(pair, timeframe, period*2)
    if err != nil {
        return 0, 0, 0, fmt.Errorf("failed to get candles for Bollinger Bands: %w", err)
    }
    
    closes := make([]float64, len(candles))
    for i, candle := range candles {
        closes[i] = candle.ClosePrice
    }
    
    upperBand, middleBand, lowerBand := indicator.BollingerBands(closes, period, k)
    
    if len(upperBand) == 0 {
        return 0, 0, 0, fmt.Errorf("Bollinger Bands calculation returned no values")
    }
    
    lastIdx := len(upperBand) - 1
    return upperBand[lastIdx], middleBand[lastIdx], lowerBand[lastIdx], nil
}

// Volatilité (refactorisée mais optionnelle vs la librairie)
func (c *Calculator) CalculateVolatility(pair, timeframe string, period int) (float64, error) {
    candles, err := c.db.GetCandles(pair, timeframe, period)
    if err != nil {
        return 0, fmt.Errorf("failed to get candles for volatility: %w", err)
    }
    
    if len(candles) < 2 {
        return 0, fmt.Errorf("not enough candles for volatility calculation")
    }
    
    closes := make([]float64, len(candles))
    for i, candle := range candles {
        closes[i] = candle.ClosePrice
    }
    
    // Utiliser Standard Deviation de la librairie
    volatility := indicator.StandardDeviation(period, closes)
    if len(volatility) == 0 {
        return 0, fmt.Errorf("volatility calculation returned no values")
    }
    
    return volatility[len(volatility)-1] * 100, nil // Convertir en pourcentage
}
```

### TradingContext Enrichi avec Indicateurs
```go
// internal/algorithms/algorithm.go
type TradingContext struct {
    Pair          string
    CurrentPrice  float64
    Balance       map[string]float64
    OpenPositions []database.Position
    Calculator    *market.Calculator  // Service de calcul d'indicateurs
}

type IndicatorValues struct {
    RSI        float64
    Volatility float64
    // Futurs indicateurs
    MACD       *struct{ MACD, Signal, Histogram float64 }
    Bollinger  *struct{ Upper, Middle, Lower float64 }
    SMA20      float64
    EMA50      float64
}
```

### Algorithme RSI-DCA Simplifié
```go
// internal/algorithms/rsi_dca.go
package algorithms

import (
    "github.com/cinar/indicator"
    "bot/internal/core/database"
    "bot/internal/market"
)

type RSI_DCA struct{}

func (a *RSI_DCA) ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error) {
    // 1. Calculer RSI avec la librairie (super clean!)
    rsi, err := ctx.Calculator.CalculateRSI(ctx.Pair, strategy.RSITimeframe, strategy.RSIPeriod)
    if err != nil {
        return BuySignal{}, fmt.Errorf("failed to calculate RSI: %w", err)
    }
    
    // 2. Calculer volatilité avec la librairie
    volatility, err := ctx.Calculator.CalculateVolatility(ctx.Pair, strategy.VolatilityTimeframe, strategy.VolatilityPeriod)
    if err != nil {
        return BuySignal{}, fmt.Errorf("failed to calculate volatility: %w", err)
    }
    
    // 3. Logique d'achat (inchangée)
    if rsi > strategy.RSIThreshold {
        return BuySignal{
            ShouldBuy: false, 
            Reason:    fmt.Sprintf("RSI %.2f > threshold %.2f", rsi, strategy.RSIThreshold),
        }, nil
    }
    
    // 4. Prix cible pré-calculé (logique existante)
    volatilityFactor := (volatility - strategy.ProfitTarget) / 100.0
    adjustmentPercent := volatilityFactor * (strategy.VolatilityAdjustment / 100.0)
    dynamicProfitPercent := (strategy.ProfitTarget / 100.0) + adjustmentPercent
    
    // Clamp entre 0.1% et 10%
    if dynamicProfitPercent < 0.001 {
        dynamicProfitPercent = 0.001
    } else if dynamicProfitPercent > 0.10 {
        dynamicProfitPercent = 0.10
    }
    
    // Prix d'achat avec offset dynamique
    dynamicOffsetPercent := -((0.1 / 100.0) + (rsi/100.0)/100.0)
    dynamicOffset := ctx.CurrentPrice * dynamicOffsetPercent
    limitPrice := ctx.CurrentPrice + dynamicOffset
    
    targetPrice := limitPrice * (1.0 + dynamicProfitPercent)
    
    return BuySignal{
        ShouldBuy:   true,
        Amount:      strategy.QuoteAmount / limitPrice,
        LimitPrice:  limitPrice,
        TargetPrice: targetPrice,
        Reason:      fmt.Sprintf("RSI %.2f < threshold %.2f, volatility %.2f%%, target profit %.2f%%", 
                                rsi, strategy.RSIThreshold, volatility, dynamicProfitPercent*100),
    }, nil
}

func (a *RSI_DCA) ShouldSell(ctx TradingContext, position database.Position, strategy database.Strategy) (SellSignal, error) {
    // Logique de vente existante (trailing stop) - inchangée
    if ctx.CurrentPrice >= position.TargetPrice {
        trailingStopThreshold := 1.0 - (strategy.TrailingStopDelta / 100)
        if ctx.CurrentPrice < (position.MaxPrice * trailingStopThreshold) {
            priceOffset := ctx.CurrentPrice * (strategy.SellOffset / 100.0)
            limitPrice := ctx.CurrentPrice + priceOffset
            
            return SellSignal{
                ShouldSell: true,
                LimitPrice: limitPrice,
                Reason: fmt.Sprintf("Trailing stop: %.4f < %.4f (max: %.4f)", 
                    ctx.CurrentPrice, position.MaxPrice * trailingStopThreshold, position.MaxPrice),
            }, nil
        }
    }
    
    return SellSignal{ShouldSell: false}, nil
}
```

## 🚀 Nouvelles Stratégies Possibles avec la Librairie

### Stratégie MACD Cross
```go
// internal/algorithms/macd_cross.go
type MACD_Cross struct{}

func (a *MACD_Cross) ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error) {
    macd, signal, _, err := ctx.Calculator.CalculateMACD(ctx.Pair, "4h", 12, 26, 9)
    if err != nil {
        return BuySignal{}, err
    }
    
    // Signal d'achat : MACD croise au-dessus de la ligne de signal
    if macd > signal {
        return BuySignal{
            ShouldBuy: true,
            Amount: strategy.QuoteAmount / ctx.CurrentPrice,
            LimitPrice: ctx.CurrentPrice,
            TargetPrice: ctx.CurrentPrice * (1.0 + strategy.ProfitTarget/100.0),
            Reason: fmt.Sprintf("MACD cross above signal: %.4f > %.4f", macd, signal),
        }, nil
    }
    
    return BuySignal{ShouldBuy: false, Reason: "MACD below signal line"}, nil
}
```

### Stratégie Bollinger Bands Mean Reversion
```go
// internal/algorithms/bollinger_reversion.go
type BollingerReversion struct{}

func (a *BollingerReversion) ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error) {
    upper, middle, lower, err := ctx.Calculator.CalculateBollingerBands(ctx.Pair, "1h", 20, 2.0)
    if err != nil {
        return BuySignal{}, err
    }
    
    // Signal d'achat : prix touche la bande inférieure (oversold)
    if ctx.CurrentPrice <= lower*1.01 { // 1% de marge
        return BuySignal{
            ShouldBuy: true,
            Amount: strategy.QuoteAmount / ctx.CurrentPrice,
            LimitPrice: ctx.CurrentPrice,
            TargetPrice: middle, // Target = ligne médiane
            Reason: fmt.Sprintf("Price %.4f near lower band %.4f", ctx.CurrentPrice, lower),
        }, nil
    }
    
    return BuySignal{ShouldBuy: false, Reason: "Price not near lower Bollinger band"}, nil
}
```

## 📊 Structure Database avec Algorithmes Étendus

### Table Strategies Étendue
```sql
CREATE TABLE strategies (
    -- ... colonnes existantes ...
    algorithm_name TEXT NOT NULL DEFAULT 'rsi_dca',
    
    -- Paramètres RSI (pour rsi_dca)
    rsi_threshold REAL,
    rsi_period INTEGER,
    rsi_timeframe TEXT,
    
    -- Paramètres MACD (pour macd_cross)
    macd_fast_period INTEGER DEFAULT 12,
    macd_slow_period INTEGER DEFAULT 26,
    macd_signal_period INTEGER DEFAULT 9,
    macd_timeframe TEXT DEFAULT '4h',
    
    -- Paramètres Bollinger (pour bollinger_reversion) 
    bb_period INTEGER DEFAULT 20,
    bb_multiplier REAL DEFAULT 2.0,
    bb_timeframe TEXT DEFAULT '1h',
    
    -- Paramètres génériques (utilisés par tous les algos)
    profit_target REAL NOT NULL,
    trailing_stop_delta REAL NOT NULL,
    sell_offset REAL NOT NULL,
    volatility_period INTEGER,
    volatility_adjustment REAL,
    volatility_timeframe TEXT
);
```

### Exemples de Stratégies
```sql
-- RSI Strategy (existante, migrée)
INSERT INTO strategies (name, algorithm_name, cron_expression, quote_amount,
    rsi_threshold, rsi_period, rsi_timeframe, profit_target, trailing_stop_delta, sell_offset) 
VALUES ('RSI Scalping', 'rsi_dca', '0 */6 * * *', 25.0, 
    70.0, 14, '4h', 2.0, 0.1, 0.1);

-- MACD Strategy (nouvelle)
INSERT INTO strategies (name, algorithm_name, cron_expression, quote_amount,
    macd_fast_period, macd_slow_period, macd_signal_period, macd_timeframe, profit_target, trailing_stop_delta, sell_offset)
VALUES ('MACD Cross 4h', 'macd_cross', '0 */4 * * *', 50.0,
    12, 26, 9, '4h', 3.0, 0.2, 0.1);

-- Bollinger Strategy (nouvelle)  
INSERT INTO strategies (name, algorithm_name, cron_expression, quote_amount,
    bb_period, bb_multiplier, bb_timeframe, profit_target, trailing_stop_delta, sell_offset)
VALUES ('Bollinger Mean Reversion', 'bollinger_reversion', '0 */2 * * *', 30.0,
    20, 2.0, '1h', 1.5, 0.1, 0.1);
```

## 🎯 Bénéfices de l'Intégration de la Librairie

### ✅ **Robustesse**
- Implémentations testées et validées
- Gestion des edge cases
- Performance optimisée

### ✅ **Productivité** 
- Plus besoin de réinventer la roue
- Focus sur la logique métier
- Développement stratégies plus rapide

### ✅ **Extensibilité**
- 50+ indicateurs disponibles
- Stratégies multi-indicateurs possibles
- Backtesting avec indicateurs standards

### ✅ **Maintenance**
- Mise à jour via `go get -u`
- Bug fixes automatiques
- Communauté active

## 🔄 Migration Path

1. **Ajouter la dépendance** : `go get github.com/cinar/indicator`
2. **Remplacer les calculs manuels** par les appels à la librairie  
3. **Étendre les algorithmes** avec de nouveaux indicateurs
4. **Interface web** avec sélection d'algorithmes et paramètres correspondants

Cette intégration transforme le bot en véritable plateforme de trading algorithmique professionnelle ! 🚀