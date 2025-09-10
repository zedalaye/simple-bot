# Scheduler avec github.com/go-co-op/gocron

## 🎯 Mise à Jour de l'Architecture Scheduler

### Avantages de gocron vs robfig/cron
- ✅ **API plus simple** et intuitive
- ✅ **Maintenance active** (dernière release récente)
- ✅ **Fonctionnalités avancées** : jobs uniques, timeouts, retry logic
- ✅ **Meilleure gestion des erreurs**
- ✅ **Support des tags** pour grouper les jobs
- ✅ **Monitoring intégré**

## 📦 Installation et Usage

### Dépendance
```bash
go get github.com/go-co-op/gocron/v2
```

### Implémentation du Scheduler

```go
// internal/scheduler/scheduler.go
package scheduler

import (
    "context"
    "fmt"
    "time"
    
    "github.com/go-co-op/gocron/v2"
    "bot/internal/core/database"
    "bot/internal/logger"
)

type StrategyScheduler struct {
    scheduler gocron.Scheduler
    db        *database.DB
    ctx       context.Context
    cancel    context.CancelFunc
}

func NewStrategyScheduler(db *database.DB) (*StrategyScheduler, error) {
    // Créer le scheduler avec options
    s, err := gocron.NewScheduler(
        gocron.WithLocation(time.UTC),
        gocron.WithLogger(gocron.NewLogger(gocron.LogLevelInfo)),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create scheduler: %w", err)
    }
    
    ctx, cancel := context.WithCancel(context.Background())
    
    return &StrategyScheduler{
        scheduler: s,
        db:        db,
        ctx:       ctx,
        cancel:    cancel,
    }, nil
}

// Démarrer le scheduler avec toutes les stratégies actives
func (ss *StrategyScheduler) Start() error {
    logger.Info("Starting strategy scheduler...")
    
    // Charger toutes les stratégies actives
    strategies, err := ss.db.GetEnabledStrategies()
    if err != nil {
        return fmt.Errorf("failed to load strategies: %w", err)
    }
    
    // Programmer chaque stratégie
    for _, strategy := range strategies {
        err := ss.ScheduleStrategy(strategy)
        if err != nil {
            logger.Errorf("Failed to schedule strategy %s: %v", strategy.Name, err)
            continue
        }
        logger.Infof("Scheduled strategy '%s' with cron '%s'", strategy.Name, strategy.CronExpression)
    }
    
    // Démarrer le scheduler
    ss.scheduler.Start()
    
    return nil
}

// Programmer une stratégie individuelle
func (ss *StrategyScheduler) ScheduleStrategy(strategy database.Strategy) error {
    // Créer le job avec cron expression
    job, err := ss.scheduler.NewJob(
        gocron.CronJob(strategy.CronExpression, false), // false = pas de secondes
        gocron.NewTask(ss.executeStrategy, strategy.ID),
        gocron.WithTags(fmt.Sprintf("strategy-%d", strategy.ID)),
        gocron.WithName(strategy.Name),
    )
    
    if err != nil {
        return fmt.Errorf("failed to create job for strategy %s: %w", strategy.Name, err)
    }
    
    logger.Infof("Job created for strategy %s (ID: %d)", strategy.Name, strategy.ID)
    return nil
}

// Fonction d'exécution d'une stratégie (appelée par le scheduler)
func (ss *StrategyScheduler) executeStrategy(strategyID int) {
    logger.Infof("Executing strategy ID: %d", strategyID)
    
    // TODO: Cette logique sera implémentée dans les phases suivantes
    // 1. Charger la stratégie depuis la DB
    // 2. Charger l'algorithme correspondant  
    // 3. Créer le TradingContext
    // 4. Exécuter la logique d'achat/vente
    // 5. Mettre à jour last_executed_at
    
    // Pour l'instant, juste logger
    strategy, err := ss.db.GetStrategy(strategyID)
    if err != nil {
        logger.Errorf("Failed to load strategy %d: %v", strategyID, err)
        return
    }
    
    logger.Infof("Strategy '%s' executed successfully", strategy.Name)
    
    // Mettre à jour la dernière exécution
    now := time.Now()
    err = ss.db.UpdateStrategyExecution(strategyID, now)
    if err != nil {
        logger.Errorf("Failed to update strategy execution time: %v", err)
    }
}

// Ajouter une nouvelle stratégie au runtime
func (ss *StrategyScheduler) AddStrategy(strategy database.Strategy) error {
    return ss.ScheduleStrategy(strategy)
}

// Supprimer une stratégie du scheduler
func (ss *StrategyScheduler) RemoveStrategy(strategyID int) error {
    // Utiliser les tags pour trouver et supprimer le job
    tag := fmt.Sprintf("strategy-%d", strategyID)
    err := ss.scheduler.RemoveByTags(tag)
    if err != nil {
        return fmt.Errorf("failed to remove strategy %d: %w", strategyID, err)
    }
    
    logger.Infof("Strategy %d removed from scheduler", strategyID)
    return nil
}

// Mettre à jour une stratégie (supprimer + recréer)
func (ss *StrategyScheduler) UpdateStrategy(strategy database.Strategy) error {
    // Supprimer l'ancien job
    err := ss.RemoveStrategy(strategy.ID)
    if err != nil {
        return fmt.Errorf("failed to remove old strategy job: %w", err)
    }
    
    // Recréer avec nouveaux paramètres
    return ss.AddStrategy(strategy)
}

// Arrêter le scheduler proprement
func (ss *StrategyScheduler) Stop() error {
    logger.Info("Stopping strategy scheduler...")
    
    ss.cancel()
    
    err := ss.scheduler.Shutdown()
    if err != nil {
        return fmt.Errorf("failed to shutdown scheduler: %w", err)
    }
    
    logger.Info("Strategy scheduler stopped")
    return nil
}

// Obtenir des statistiques du scheduler
func (ss *StrategyScheduler) GetStats() map[string]interface{} {
    jobs := ss.scheduler.Jobs()
    
    stats := map[string]interface{}{
        "total_jobs": len(jobs),
        "running":    ss.scheduler.IsRunning(),
        "jobs":       make([]map[string]interface{}, len(jobs)),
    }
    
    for i, job := range jobs {
        stats["jobs"].([]map[string]interface{})[i] = map[string]interface{}{
            "name":      job.Name(),
            "tags":      job.Tags(),
            "last_run":  job.LastRun(),
            "next_run":  job.NextRun(),
        }
    }
    
    return stats
}
```

## 🔧 Intégration dans le Bot Principal

### Modification du main.go
```go
// cmd/bot/main.go (modifié)
package main

import (
    "bot/internal/bot"
    "bot/internal/loader"
    "bot/internal/scheduler"
    "bot/internal/logger"
    // ... autres imports
)

func main() {
    // ... code existant jusqu'à la création du bot ...
    
    // Créer le scheduler de stratégies
    strategyScheduler, err := scheduler.NewStrategyScheduler(db)
    if err != nil {
        log.Fatalf("Failed to create strategy scheduler: %v", err)
    }
    
    // Démarrer le scheduler
    err = strategyScheduler.Start()
    if err != nil {
        log.Fatalf("Failed to start strategy scheduler: %v", err)
    }
    
    // Démarrer le bot traditionnel (pour la compatibilité)
    err = tradingBot.Start(*buyAtLaunch)
    if err != nil {
        logger.Fatalf("Failed to start bot: %v", err)
    }
    
    // Gestion des signaux d'arrêt
    waitForShutdown(tradingBot, strategyScheduler)
}

func waitForShutdown(tradingBot *bot.Bot, strategyScheduler *scheduler.StrategyScheduler) {
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

    <-sigs
    logger.Info("Got a stop signal. Stopping services...")

    // Arrêter le scheduler en premier
    if err := strategyScheduler.Stop(); err != nil {
        logger.Errorf("Error stopping scheduler: %v", err)
    }
    
    // Puis arrêter le bot
    tradingBot.Stop()
    time.Sleep(1 * time.Second)

    tradingBot.ShowStatistics()
    logger.Info("Simple Bot Stopped. See Ya!")
}
```

## 📊 Exemples d'Usage avec gocron

### Syntaxe Cron Supportée
```go
// Expressions cron classiques
"0 9 * * *"      // Tous les jours à 9h
"0 */6 * * *"    // Toutes les 6 heures
"0 10 1 * *"     // Le 1er de chaque mois à 10h
"*/30 * * * *"   // Toutes les 30 minutes

// API alternative (plus lisible)
scheduler.Every(6).Hours().At("09:30")
scheduler.Every(1).Day().At("09:00")  
scheduler.Every(30).Minutes()
```

### Programmation des Stratégies
```go
// Stratégies d'exemple avec gocron
strategies := []database.Strategy{
    {
        Name: "Daily Conservative",
        CronExpression: "0 9 * * *",  // 9h tous les jours
        QuoteAmount: 15.0,
        // ... autres paramètres
    },
    {
        Name: "Scalping Fast", 
        CronExpression: "*/15 * * * *",  // Toutes les 15 minutes
        QuoteAmount: 5.0,
        // ... autres paramètres
    },
    {
        Name: "Monthly Swing",
        CronExpression: "0 10 1 * *",  // 1er du mois à 10h
        QuoteAmount: 100.0,
        // ... autres paramètres
    },
}
```

## 🎯 Avantages de gocron pour notre Use Case

### ✅ **Gestion Dynamique**
```go
// Ajouter une stratégie à chaud
newStrategy := database.Strategy{Name: "New Strategy", CronExpression: "0 */2 * * *"}
scheduler.AddStrategy(newStrategy)

// Modifier une stratégie existante
updatedStrategy.CronExpression = "0 */4 * * *" 
scheduler.UpdateStrategy(updatedStrategy)

// Supprimer une stratégie
scheduler.RemoveStrategy(strategyID)
```

### ✅ **Monitoring Intégré**
```go
// Statistiques du scheduler
stats := scheduler.GetStats()
logger.Infof("Active jobs: %d", stats["total_jobs"])

// Prochaines exécutions
for _, job := range stats["jobs"].([]map[string]interface{}) {
    logger.Infof("Job '%s' next run: %v", job["name"], job["next_run"])
}
```

### ✅ **Gestion des Erreurs**
```go
// Avec timeout et retry
job, err := scheduler.NewJob(
    gocron.CronJob("0 */6 * * *", false),
    gocron.NewTask(executeStrategy, strategyID),
    gocron.WithEventListeners(
        gocron.AfterJobRuns(func(jobID uuid.UUID, jobName string) {
            logger.Infof("Job %s completed successfully", jobName)
        }),
        gocron.AfterJobRunsWithError(func(jobID uuid.UUID, jobName string, err error) {
            logger.Errorf("Job %s failed: %v", jobName, err)
        }),
    ),
)
```

Cette librairie rend l'implémentation du scheduler beaucoup plus robuste et maintenable ! 🚀