# Architecture Finale - Stratégies Multiples + Market Data + Backtesting

## 🎯 Problèmes Résolus par le Stockage des Bougies

### Problèmes Actuels Identifiés
```go
// bot.go:604 - Paramètres hardcodés problématiques
candles, err := b.exchange.FetchCandles(b.Config.Pair, "4h", &since, int64(period*6))

// bot.go:650 - Timeframe et limite hardcodés
candles, err := b.exchange.FetchCandles(b.Config.Pair, "4h", &since, 500)
```

### Solutions Apportées
- ✅ **Flexibilité timeframes** : 1m, 5m, 15m, 1h, 4h, 1d configurables par stratégie
- ✅ **Performance** : Pas de refetch constant, cache en DB
- ✅ **Backtesting** : Historique complet disponible
- ✅ **Économie API** : Fetch uniquement les nouvelles bougies
- ✅ **Multi-pairs** : Support futur de plusieurs paires

## 🗄️ Nouvelle Structure Base de Données

### Schema Complet avec Market Data
```sql
-- Migration 6: Table candles pour historique
CREATE TABLE candles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pair TEXT NOT NULL,                    -- UBTC/USDC, BTC/USDC, etc.
    timeframe TEXT NOT NULL,               -- 1m, 5m, 15m, 1h, 4h, 1d
    timestamp INTEGER NOT NULL,            -- Unix timestamp
    open_price REAL NOT NULL,
    high_price REAL NOT NULL,
    low_price REAL NOT NULL,
    close_price REAL NOT NULL,
    volume REAL NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    
    -- Contrainte d'unicité pour éviter les doublons
    UNIQUE(pair, timeframe, timestamp)
);

-- Index pour les requêtes performantes
CREATE INDEX idx_candles_pair_timeframe_timestamp ON candles(pair, timeframe, timestamp);
CREATE INDEX idx_candles_timestamp ON candles(timestamp);

-- Migration 7: Table strategies avec timeframes configurables
CREATE TABLE strategies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    enabled BOOLEAN DEFAULT 1,
    
    -- Configuration de base
    algorithm_name TEXT NOT NULL DEFAULT 'rsi_dca',
    cron_expression TEXT NOT NULL,
    quote_amount REAL NOT NULL,
    max_concurrent_orders INTEGER DEFAULT 1,
    
    -- Paramètres RSI configurables
    rsi_threshold REAL NOT NULL,
    rsi_period INTEGER NOT NULL,
    rsi_timeframe TEXT NOT NULL DEFAULT '4h',    -- NOUVEAU !
    
    -- Paramètres Volatilité configurables  
    volatility_period INTEGER NOT NULL,
    volatility_adjustment REAL NOT NULL,
    volatility_timeframe TEXT NOT NULL DEFAULT '4h',  -- NOUVEAU !
    
    -- Paramètres de vente
    profit_target REAL NOT NULL,
    trailing_stop_delta REAL NOT NULL,
    sell_offset REAL NOT NULL,
    
    -- Scheduling
    last_executed_at DATETIME NULL,
    next_execution_at DATETIME NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Migration 8: strategy_id sur tables existantes (comme avant)
ALTER TABLE orders ADD COLUMN strategy_id INTEGER REFERENCES strategies(id);
ALTER TABLE positions ADD COLUMN strategy_id INTEGER REFERENCES strategies(id);
ALTER TABLE cycles ADD COLUMN strategy_id INTEGER REFERENCES strategies(id);

-- Migration 9: Stratégie legacy + données existantes
INSERT INTO strategies (name, description, algorithm_name, cron_expression, 
    quote_amount, rsi_threshold, rsi_period, rsi_timeframe,
    volatility_period, volatility_adjustment, volatility_timeframe,
    profit_target, trailing_stop_delta, sell_offset) 
VALUES ('Legacy Strategy', 'Migrated from config file', 'rsi_dca', '0 */4 * * *', 
    50.0, 70.0, 14, '4h', 
    7, 50.0, '4h',
    2.0, 0.1, 0.1);

UPDATE orders SET strategy_id = 1 WHERE strategy_id IS NULL;
UPDATE positions SET strategy_id = 1 WHERE strategy_id IS NULL;
UPDATE cycles SET strategy_id = 1 WHERE strategy_id IS NULL;
```

## 🔧 Nouveau Service MarketDataCollector

### Structure
```
internal/
├── market/
│   ├── collector.go        # Service de collecte des bougies
│   ├── calculator.go       # RSI, Volatility, etc. (refactorisés)
│   └── backtester.go       # Engine de backtesting (futur)
```

### Service Collector
```go
// internal/market/collector.go
package market

type MarketDataCollector struct {
    db       *database.DB
    exchange bot.Exchange
}

func NewMarketDataCollector(db *database.DB, exchange bot.Exchange) *MarketDataCollector {
    return &MarketDataCollector{db: db, exchange: exchange}
}

// Collecte les bougies manquantes pour une paire/timeframe
func (mdc *MarketDataCollector) CollectCandles(pair, timeframe string, limit int) error {
    // 1. Récupérer la dernière bougie en DB
    lastCandle, err := mdc.db.GetLastCandle(pair, timeframe)
    
    // 2. Calculer depuis quand fetch (ou historique complet si première fois)  
    var since *int64
    if lastCandle != nil {
        since = &lastCandle.Timestamp
    }
    
    // 3. Fetch depuis l'exchange
    candles, err := mdc.exchange.FetchCandles(pair, timeframe, since, int64(limit))
    if err != nil {
        return fmt.Errorf("failed to fetch candles: %w", err)
    }
    
    // 4. Sauver en DB (avec gestion des doublons)
    return mdc.db.SaveCandles(pair, timeframe, candles)
}

// Collecte automatique pour toutes les paires/timeframes actives
func (mdc *MarketDataCollector) CollectAllActiveTimeframes() error {
    activeTimeframes, err := mdc.db.GetActiveTimeframes()
    if err != nil {
        return err
    }
    
    for _, tf := range activeTimeframes {
        err := mdc.CollectCandles(tf.Pair, tf.Timeframe, 100)
        if err != nil {
            logger.Errorf("Failed to collect %s/%s: %v", tf.Pair, tf.Timeframe, err)
        }
    }
    
    return nil
}
```

### Calculator Refactorisé
```go
// internal/market/calculator.go
package market

// RSI calculé depuis la DB au lieu de l'exchange
func (mdc *MarketDataCollector) CalculateRSI(pair, timeframe string, period int) (float64, error) {
    // Récupérer les bougies depuis la DB
    candles, err := mdc.db.GetCandles(pair, timeframe, period+1)
    if err != nil {
        return 0, fmt.Errorf("failed to get candles for RSI: %w", err)
    }
    
    if len(candles) < period+1 {
        return 0, fmt.Errorf("not enough candles for RSI calculation: need %d, got %d", period+1, len(candles))
    }
    
    // Logique RSI existante mais avec données de la DB
    gains := make([]float64, len(candles)-1)
    losses := make([]float64, len(candles)-1)
    
    for i := 1; i < len(candles); i++ {
        change := candles[i].ClosePrice - candles[i-1].ClosePrice
        if change > 0 {
            gains[i-1] = change
            losses[i-1] = 0
        } else {
            gains[i-1] = 0
            losses[i-1] = -change
        }
    }
    
    // ... reste du calcul RSI ...
}

// Volatilité calculée depuis la DB
func (mdc *MarketDataCollector) CalculateVolatility(pair, timeframe string, period int) (float64, error) {
    candles, err := mdc.db.GetCandles(pair, timeframe, period)
    if err != nil {
        return 0, fmt.Errorf("failed to get candles for volatility: %w", err)
    }
    
    if len(candles) < 2 {
        return 0, fmt.Errorf("not enough candles for volatility calculation")
    }
    
    // Logique volatilité existante mais avec données de la DB
    prices := make([]float64, len(candles))
    for i, candle := range candles {
        prices[i] = candle.ClosePrice
    }
    
    // ... reste du calcul volatilité ...
}
```

## 📊 Nouvelles Méthodes Database

### CRUD Candles
```go
// Sauvegarder des bougies (avec gestion doublons)
func (db *DB) SaveCandles(pair, timeframe string, candles []bot.Candle) error

// Récupérer les N dernières bougies
func (db *DB) GetCandles(pair, timeframe string, limit int) ([]database.Candle, error)

// Récupérer la dernière bougie pour une paire/timeframe
func (db *DB) GetLastCandle(pair, timeframe string) (*database.Candle, error)

// Récupérer toutes les combinaisons paire/timeframe actives
func (db *DB) GetActiveTimeframes() ([]database.ActiveTimeframe, error)

// Nettoyer les anciennes bougies (garder X jours)
func (db *DB) CleanupOldCandles(olderThanDays int) error
```

### Types Database
```go
type Candle struct {
    ID          int       `json:"id" db:"id"`
    Pair        string    `json:"pair" db:"pair"`
    Timeframe   string    `json:"timeframe" db:"timeframe"`
    Timestamp   int64     `json:"timestamp" db:"timestamp"`
    OpenPrice   float64   `json:"open_price" db:"open_price"`
    HighPrice   float64   `json:"high_price" db:"high_price"`
    LowPrice    float64   `json:"low_price" db:"low_price"`
    ClosePrice  float64   `json:"close_price" db:"close_price"`
    Volume      float64   `json:"volume" db:"volume"`
    CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type ActiveTimeframe struct {
    Pair      string `db:"pair"`
    Timeframe string `db:"timeframe"`
}
```

## 🔄 Algorithme RSI-DCA Mis à Jour

### Avec Market Data Service
```go
// internal/algorithms/rsi_dca.go (mis à jour)

func (a *RSI_DCA) ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error) {
    // RSI calculé avec les paramètres de la stratégie !
    rsi, err := ctx.MarketData.CalculateRSI(ctx.Pair, strategy.RSITimeframe, strategy.RSIPeriod)
    if err != nil {
        return BuySignal{}, fmt.Errorf("failed to calculate RSI: %w", err)
    }
    
    // Volatilité calculée avec les paramètres de la stratégie !
    volatility, err := ctx.MarketData.CalculateVolatility(ctx.Pair, strategy.VolatilityTimeframe, strategy.VolatilityPeriod)
    if err != nil {
        return BuySignal{}, fmt.Errorf("failed to calculate volatility: %w", err)
    }
    
    if rsi > strategy.RSIThreshold {
        return BuySignal{ShouldBuy: false, Reason: "RSI too high"}, nil
    }
    
    // Prix cible pré-calculé avec volatilité DB
    volatilityFactor := (volatility - strategy.ProfitTarget) / 100.0
    adjustmentPercent := volatilityFactor * (strategy.VolatilityAdjustment / 100.0)
    dynamicProfitPercent := (strategy.ProfitTarget / 100.0) + adjustmentPercent
    
    // ... reste de la logique ...
}
```

### TradingContext Enrichi
```go
type TradingContext struct {
    Pair          string
    CurrentPrice  float64
    Balance       map[string]float64
    OpenPositions []database.Position
    MarketData    *market.MarketDataCollector  // NOUVEAU !
}
```

## 🚀 Configuration Finale

### Config.yml Ultra-Minimal
```yaml
exchange:
  name: hyperliquid

database:
  path: db/bot.db

logging:
  level: info
  file: ""

web:
  port: ":8080"

global:
  pair: UBTC/USDC
  check_interval_minutes: 5
  market_data_collection_minutes: 30  # Collecte bougies toutes les 30min
```

### Exemple de Stratégies en DB
```sql
-- Stratégie scalping 1min
INSERT INTO strategies (name, algorithm_name, cron_expression, rsi_timeframe, volatility_timeframe, ...) 
VALUES ('Scalping 1m', 'rsi_dca', '0 */15 * * *', '1m', '5m', ...);

-- Stratégie swing 1d  
INSERT INTO strategies (name, algorithm_name, cron_expression, rsi_timeframe, volatility_timeframe, ...)
VALUES ('Swing Daily', 'rsi_dca', '0 9 * * *', '1d', '1d', ...);
```

## 🎯 Bénéfices de cette Architecture

### ✅ **Performance Optimale**
- Cache des bougies en DB
- Calculs RSI/Volatility sur données locales
- Économie d'appels API

### ✅ **Flexibilité Totale**  
- Timeframes configurables par stratégie
- Paramètres RSI/Volatility indépendants
- Support multi-pairs futur

### ✅ **Backtesting Ready**
- Historique complet des bougies
- Replay possible de n'importe quelle période
- Validation des stratégies avant déploiement

### ✅ **Évolutivité**
- Nouveaux indicateurs faciles (MACD, Bollinger, etc.)
- Algorithmes utilisant plusieurs timeframes
- Analyse technique avancée

Cette architecture résout élégamment les problèmes hardcodés tout en ouvrant la voie au backtesting et à des stratégies beaucoup plus sophistiquées !