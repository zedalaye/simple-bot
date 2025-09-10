# Structure de Base de Données Révisée pour les Stratégies Multiples

## 📊 Analyse du Schéma Existant

Après examen du code [`internal/core/database/database.go`](internal/core/database/database.go:1), voici le schéma actuel :

### Tables Existantes
- **`positions`** : `id`, `price`, `amount`, `max_price`, `target_price`, `created_at`, `updated_at`
- **`orders`** : `id`, `external_id`, `side`, `amount`, `price`, `fees`, `status`, `position_id`, `created_at`, `updated_at`
- **`cycles`** : `id`, `buy_order_id`, `sell_order_id`, `created_at`, `updated_at`
- **`migrations`** : système de migrations existant (IDs 1-5 déjà utilisés)

## 🏗️ Nouvelle Structure Proposée (Révisée)

### Migration 6 : Table Strategies
```sql
CREATE TABLE IF NOT EXISTS strategies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    enabled BOOLEAN DEFAULT 1,
    -- Configuration complète en JSON pour flexibilité
    config JSON NOT NULL,
    -- Statistiques de performance
    total_orders INTEGER DEFAULT 0,
    successful_orders INTEGER DEFAULT 0,
    total_profit REAL DEFAULT 0.0,
    -- Timestamps
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    -- Dernière exécution pour le cron
    last_executed_at DATETIME NULL,
    next_execution_at DATETIME NULL
);

-- Index pour les performances
CREATE INDEX IF NOT EXISTS idx_strategies_enabled ON strategies(enabled);
CREATE INDEX IF NOT EXISTS idx_strategies_next_execution ON strategies(next_execution_at);
```

### Migration 7 : Ajout strategy_id aux tables existantes
```sql
-- Ajouter strategy_id à orders
ALTER TABLE orders ADD COLUMN strategy_id INTEGER NULL REFERENCES strategies(id) ON DELETE SET NULL;

-- Ajouter strategy_id à positions  
ALTER TABLE positions ADD COLUMN strategy_id INTEGER NULL REFERENCES strategies(id) ON DELETE SET NULL;

-- Ajouter strategy_id à cycles
ALTER TABLE cycles ADD COLUMN strategy_id INTEGER NULL REFERENCES strategies(id) ON DELETE SET NULL;

-- Index pour les performances
CREATE INDEX IF NOT EXISTS idx_orders_strategy_id ON orders(strategy_id);
CREATE INDEX IF NOT EXISTS idx_positions_strategy_id ON positions(strategy_id);
CREATE INDEX IF NOT EXISTS idx_cycles_strategy_id ON cycles(strategy_id);
```

### Migration 8 : Stratégie par défaut + Migration données existantes
```sql
-- Créer une stratégie "Legacy" pour les données existantes
INSERT INTO strategies (id, name, description, enabled, config) 
VALUES (1, 'Legacy Strategy', 'Migrated from single-strategy configuration', 1, '{}');

-- Assigner toutes les données existantes à la stratégie Legacy
UPDATE orders SET strategy_id = 1 WHERE strategy_id IS NULL;
UPDATE positions SET strategy_id = 1 WHERE strategy_id IS NULL;
UPDATE cycles SET strategy_id = 1 WHERE strategy_id IS NULL;

-- Rendre strategy_id obligatoire maintenant que toutes les données sont migrées
-- (SQLite ne supporte pas ALTER COLUMN, donc on garde NULL mais on l'impose via l'application)
```

## 🔄 Structures Go Mises à Jour

### Strategy struct
```go
type Strategy struct {
    ID                int                    `json:"id" db:"id"`
    Name              string                 `json:"name" db:"name"`
    Description       string                 `json:"description" db:"description"`
    Enabled           bool                   `json:"enabled" db:"enabled"`
    Config            map[string]interface{} `json:"config" db:"config"` // JSON flexible
    TotalOrders       int                    `json:"total_orders" db:"total_orders"`
    SuccessfulOrders  int                    `json:"successful_orders" db:"successful_orders"`
    TotalProfit       float64                `json:"total_profit" db:"total_profit"`
    CreatedAt         time.Time              `json:"created_at" db:"created_at"`
    UpdatedAt         time.Time              `json:"updated_at" db:"updated_at"`
    LastExecutedAt    *time.Time             `json:"last_executed_at,omitempty" db:"last_executed_at"`
    NextExecutionAt   *time.Time             `json:"next_execution_at,omitempty" db:"next_execution_at"`
}

// StrategyConfig représente la configuration spécifique d'une stratégie
type StrategyConfig struct {
    Cron                 string  `json:"cron"`
    QuoteAmount          float64 `json:"quote_amount"`
    RSIThreshold         float64 `json:"rsi_threshold"`
    RSIPeriod            int     `json:"rsi_period"`
    ProfitTarget         float64 `json:"profit_target"`
    VolatilityPeriod     int     `json:"volatility_period"`
    VolatilityAdjustment float64 `json:"volatility_adjustment"`
    TrailingStopDelta    float64 `json:"trailing_stop_delta"`
    SellOffset           float64 `json:"sell_offset"`
    MaxConcurrentOrders  int     `json:"max_concurrent_orders"`
}
```

### Structures existantes étendues
```go
type Position struct {
    ID          int       `json:"id" db:"id"`
    Price       float64   `json:"price" db:"price"`
    Amount      float64   `json:"amount" db:"amount"`
    MaxPrice    float64   `json:"max_price" db:"max_price"`
    TargetPrice float64   `json:"target_price" db:"target_price"`
    StrategyID  *int      `json:"strategy_id,omitempty" db:"strategy_id"` // NOUVEAU
    CreatedAt   time.Time `json:"created_at" db:"created_at"`
    UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type Order struct {
    ID         int         `json:"id" db:"id"`
    ExternalID string      `json:"external_id" db:"external_id"`
    Side       OrderSide   `json:"side" db:"side"`
    Amount     float64     `json:"amount" db:"amount"`
    Price      float64     `json:"price" db:"price"`
    Fees       float64     `json:"fees" db:"fees"`
    Status     OrderStatus `json:"status" db:"status"`
    PositionID *int        `json:"position_id,omitempty" db:"position_id"`
    StrategyID *int        `json:"strategy_id,omitempty" db:"strategy_id"` // NOUVEAU
    CreatedAt  time.Time   `json:"created_at" db:"created_at"`
    UpdatedAt  time.Time   `json:"updated_at" db:"updated_at"`
}

type Cycle struct {
    ID        int       `json:"id" db:"id"`
    BuyOrder  Order     `json:"buy_order"`
    SellOrder *Order    `json:"sell_order,omitempty"`
    StrategyID *int     `json:"strategy_id,omitempty" db:"strategy_id"` // NOUVEAU
    CreatedAt time.Time `json:"created_at" db:"created_at"`
    UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
```

## 📈 Nouvelles Méthodes Database

### Gestion des stratégies
```go
// CRUD Strategies
func (db *DB) CreateStrategy(name, description string, config StrategyConfig) (*Strategy, error)
func (db *DB) GetStrategy(id int) (*Strategy, error)
func (db *DB) GetStrategyByName(name string) (*Strategy, error)
func (db *DB) GetAllStrategies() ([]Strategy, error)
func (db *DB) GetEnabledStrategies() ([]Strategy, error)
func (db *DB) UpdateStrategy(id int, strategy Strategy) error
func (db *DB) UpdateStrategyStats(id int, totalOrders, successfulOrders int, totalProfit float64) error
func (db *DB) UpdateStrategyExecution(id int, lastExecuted, nextExecution time.Time) error
func (db *DB) DeleteStrategy(id int) error

// Statistiques par stratégie
func (db *DB) GetStrategyStats(strategyID int) (map[string]interface{}, error)
func (db *DB) GetStrategyOrders(strategyID int) ([]Order, error)
func (db *DB) GetStrategyPositions(strategyID int) ([]Position, error)
func (db *DB) GetStrategyCycles(strategyID int) ([]Cycle, error)

// Méthodes existantes modifiées pour supporter strategy_id
func (db *DB) CreateOrderWithStrategy(externalID string, side OrderSide, amount, price, fees float64, positionID *int, strategyID int) (*Order, error)
func (db *DB) CreatePositionWithStrategy(price, targetPrice, amount float64, strategyID int) (*Position, error)
func (db *DB) CreateCycleWithStrategy(buyOrderId int, strategyID int) (*Cycle, error)
```

## 🔍 Requêtes Avancées avec Stratégies

### Statistiques globales avec détail par stratégie
```sql
-- Performance par stratégie
SELECT 
    s.name,
    s.enabled,
    COUNT(o.id) as total_orders,
    COUNT(CASE WHEN o.status = 'FILLED' THEN 1 END) as filled_orders,
    AVG(CASE WHEN c.sell_order_id IS NOT NULL THEN 
        (so.price - bo.price) * bo.amount - bo.fees - so.fees 
    END) as avg_profit,
    SUM(CASE WHEN c.sell_order_id IS NOT NULL THEN 
        (so.price - bo.price) * bo.amount - bo.fees - so.fees 
    END) as total_profit
FROM strategies s
LEFT JOIN orders o ON s.id = o.strategy_id
LEFT JOIN cycles c ON s.id = c.strategy_id
LEFT JOIN orders bo ON c.buy_order_id = bo.id
LEFT JOIN orders so ON c.sell_order_id = so.id
GROUP BY s.id, s.name, s.enabled
ORDER BY total_profit DESC;
```

### Positions actives par stratégie
```sql
SELECT 
    s.name as strategy_name,
    COUNT(p.id) as active_positions,
    SUM(p.price * p.amount) as total_invested,
    AVG(p.target_price / p.price - 1) * 100 as avg_target_profit_pct
FROM strategies s
LEFT JOIN positions p ON s.id = p.strategy_id
WHERE s.enabled = 1
  AND NOT EXISTS (
      SELECT 1 FROM orders o 
      WHERE o.position_id = p.id AND o.status = 'PENDING' AND o.side = 'SELL'
  )
GROUP BY s.id, s.name;
```

## 🚀 Migration Path Sécurisée

### Étapes de migration
1. **Migration 6** : Créer table `strategies`
2. **Migration 7** : Ajouter colonnes `strategy_id` (NULL autorisé)
3. **Migration 8** : 
   - Créer stratégie "Legacy" (ID=1)
   - Migrer toutes les données existantes vers strategy_id=1
   - Valider l'intégrité des données

### Script de validation post-migration
```sql
-- Vérifier que toutes les données ont bien une strategy_id
SELECT 'orders' as table_name, COUNT(*) as nulls FROM orders WHERE strategy_id IS NULL
UNION ALL
SELECT 'positions', COUNT(*) FROM positions WHERE strategy_id IS NULL
UNION ALL
SELECT 'cycles', COUNT(*) FROM cycles WHERE strategy_id IS NULL;
```

## 🎯 Avantages de cette Approche Révisée

1. **Compatibilité Backward** : Données existantes préservées dans "Legacy Strategy"
2. **Flexibilité Config** : JSON permet d'ajouter facilement de nouveaux paramètres
3. **Performance** : Index optimisés pour les requêtes par stratégie
4. **Statistiques** : Tracking natif des performances par stratégie
5. **Scheduling** : Colonnes `last_executed_at` et `next_execution_at` pour le cron
6. **Migration Sûre** : Processus de migration en étapes avec validation

## 🔧 Considérations Techniques

### Gestion des NULL strategy_id
- Pour les nouvelles données : `strategy_id` sera toujours fourni
- Pour les anciennes données : migrées vers strategy_id=1 (Legacy)
- Validation au niveau application pour éviter les NULL sur nouvelles données

### Performance
- Index sur `strategy_id` pour toutes les tables
- Index sur `enabled` et `next_execution_at` pour le scheduler
- Requêtes optimisées avec JOIN au lieu de sous-requêtes

### Évolutivité
- Ajout facile de nouvelles statistiques dans table `strategies`
- Configuration JSON extensible sans migration de schéma
- Support futur de meta-données par stratégie (tags, catégories, etc.)

Cette structure révisée maintient la robustesse du système existant tout en ajoutant la flexibilité nécessaire pour les stratégies multiples.