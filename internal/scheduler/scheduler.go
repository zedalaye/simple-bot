package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"bot/internal/algorithms"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"

	"github.com/go-co-op/gocron/v2"
	"github.com/robfig/cron/v3"
)

// StrategyScheduler manages cron-based execution of trading strategies
type StrategyScheduler struct {
	exchangeName    string
	pair            string
	scheduler       gocron.Scheduler
	started         bool
	db              *database.DB
	strategyManager *StrategyManager
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewStrategyScheduler creates a new strategy scheduler
func NewStrategyScheduler(exchangeName, pair string, db *database.DB, market StrategyMarket, marketCollector *market.MarketDataCollector, calculator *market.Calculator, algorithmRegistry *algorithms.AlgorithmRegistry, exchange StrategyExchange) (*StrategyScheduler, error) {
	// Create the scheduler with options
	s, err := gocron.NewScheduler(
		gocron.WithLocation(time.UTC),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create strategy manager for orchestrating execution
	strategyManager := NewStrategyManager(exchangeName, pair, db, market, marketCollector, calculator, algorithmRegistry, exchange)

	return &StrategyScheduler{
		exchangeName:    exchangeName,
		pair:            pair,
		scheduler:       s,
		started:         false,
		db:              db,
		strategyManager: strategyManager,
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// Start the scheduler and load all enabled strategies
func (ss *StrategyScheduler) Start() error {
	logger.Infof("[%s] Starting strategy scheduler...", ss.exchangeName)

	// Load all enabled strategies from database
	strategies, err := ss.db.GetEnabledStrategies()
	if err != nil {
		return fmt.Errorf("failed to load enabled strategies: %w", err)
	}

	logger.Infof("[%s] Found %d enabled strategies to schedule", ss.exchangeName, len(strategies))

	// Schedule each strategy
	for _, strategy := range strategies {
		err := ss.ScheduleStrategy(strategy)
		if err != nil {
			logger.Errorf("Failed to schedule strategy %s: %v", strategy.Name, err)
			continue
		}
		logger.Infof("[%s] ✓ Scheduled strategy '%s' (%s) with cron '%s'",
			ss.exchangeName, strategy.Name, strategy.AlgorithmName, strategy.CronExpression)
	}

	// Start the scheduler
	ss.scheduler.Start()
	logger.Infof("[%s] ✓ Strategy scheduler started successfully", ss.exchangeName)
	ss.started = true

	jobs := ss.scheduler.Jobs()
	for _, job := range jobs {
		if nextRun, err := job.NextRun(); err == nil {
			// Extraire l'ID de la stratégie depuis les tags
			tags := job.Tags()
			for _, tag := range tags {
				if strings.HasPrefix(tag, "strategy-") {
					if strategyID, parseErr := strconv.Atoi(strings.TrimPrefix(tag, "strategy-")); parseErr == nil {
						if updateErr := ss.updateStrategyNextRun(strategyID, nextRun); updateErr != nil {
							logger.Warnf("Failed to update next run time for strategy %d: %v", strategyID, updateErr)
						}
					}
					break
				}
			}
		}
	}

	return nil
}

// ScheduleStrategy schedules a single strategy with its cron expression
func (ss *StrategyScheduler) ScheduleStrategy(strategy database.Strategy) error {
	// Create the job with cron expression (simplified version)
	job, err := ss.scheduler.NewJob(
		gocron.CronJob(strategy.CronExpression, false), // false = no seconds
		gocron.NewTask(ss.executeStrategy, strategy.ID),
		gocron.WithTags(fmt.Sprintf("strategy-%d", strategy.ID)),
		gocron.WithName(strategy.Name),
	)

	if err != nil {
		return fmt.Errorf("failed to create job for strategy %s: %w", strategy.Name, err)
	}

	if ss.started {
		nextRun, err := job.NextRun()
		if err == nil {
			logger.Infof("[%s] Job created for strategy %s (ID: %d), next run: %v", ss.exchangeName, strategy.Name, strategy.ID, nextRun)
			if updateErr := ss.updateStrategyNextRun(strategy.ID, nextRun); updateErr != nil {
				logger.Warnf("Failed to update next run time for strategy %d: %v", strategy.ID, updateErr)
			}
		}
	} else {
		logger.Infof("[%s] Job created for strategy %s (ID: %d)", ss.exchangeName, strategy.Name, strategy.ID)
	}
	return nil
}

// executeStrategy is called by the scheduler to execute a strategy
func (ss *StrategyScheduler) executeStrategy(strategyID int) {
	logger.Infof("[%s] Executing strategy ID: %d", ss.exchangeName, strategyID)

	// Load strategy from database
	strategy, err := ss.db.GetStrategy(strategyID)
	if err != nil {
		logger.Errorf("Failed to load strategy %d: %v", strategyID, err)
		return
	}

	if !strategy.Enabled {
		logger.Warnf("[%s] Strategy %s is disabled, skipping execution", ss.exchangeName, strategy.Name)
		return
	}

	// Execute strategy using StrategyManager
	err = ss.strategyManager.ExecuteBuyStrategy(*strategy)
	if err != nil {
		logger.Errorf("Failed to execute strategy %s: %v", strategy.Name, err)
		return
	}

	// Update last execution time
	now := time.Now()
	err = ss.db.UpdateStrategyExecution(strategyID, now)
	if err != nil {
		logger.Errorf("Failed to update strategy execution time: %v", err)
	}

	logger.Infof("[%s] Strategy '%s' executed successfully at %v", ss.exchangeName, strategy.Name, now.Format("15:04:05"))

	var baseTime time.Time
	if strategy.NextExecutionAt != nil {
		baseTime = *strategy.NextExecutionAt
	} else {
		baseTime = now.Add(time.Minute)
	}
	nextRun, err := ss.calculateNextExecution(strategy.CronExpression, baseTime)
	if err != nil {
		logger.Warnf("[%s] Failed to calculate next run for strategy %d: %v", ss.exchangeName, strategyID, err)
	} else {
		if updateErr := ss.updateStrategyNextRun(strategyID, nextRun); updateErr != nil {
			logger.Warnf("[%s] Failed to update next run time for strategy %d: %v", ss.exchangeName, strategyID, updateErr)
		} else {
			logger.Infof("[%s] Strategy %d next run scheduled for: %v", ss.exchangeName, strategyID, nextRun.Format("15:04:05"))
		}
	}
}

func (ss *StrategyScheduler) calculateNextExecution(cronExpr string, fromTime time.Time) (time.Time, error) {
	schedule, err := cron.ParseStandard(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %s: %w", cronExpr, err)
	}

	// Calculer la prochaine exécution à partir de maintenant
	nextRun := schedule.Next(fromTime)
	return nextRun, nil
}

// AddStrategy adds a new strategy to the scheduler at runtime
func (ss *StrategyScheduler) AddStrategy(strategy database.Strategy) error {
	logger.Infof("[%s] Adding new strategy to scheduler: %s", ss.exchangeName, strategy.Name)
	return ss.ScheduleStrategy(strategy)
}

// RemoveStrategy removes a strategy from the scheduler
func (ss *StrategyScheduler) RemoveStrategy(strategyID int) error {
	// Use tags to find and remove the job
	tag := fmt.Sprintf("strategy-%d", strategyID)
	ss.scheduler.RemoveByTags(tag)

	logger.Infof("[%s] Strategy %d removed from scheduler", ss.exchangeName, strategyID)
	return nil
}

// UpdateStrategy updates a strategy in the scheduler (remove + add)
func (ss *StrategyScheduler) UpdateStrategy(strategy database.Strategy) error {
	// Remove old job
	err := ss.RemoveStrategy(strategy.ID)
	if err != nil {
		logger.Warnf("Failed to remove old strategy job (may not exist): %v", err)
	}

	// Add updated strategy
	return ss.AddStrategy(strategy)
}

// GetStrategyManager returns the strategy manager instance
func (ss *StrategyScheduler) GetStrategyManager() *StrategyManager {
	return ss.strategyManager
}

// Stop the scheduler gracefully
func (ss *StrategyScheduler) Stop() error {
	logger.Infof("[%s] Stopping strategy scheduler...", ss.exchangeName)

	ss.cancel()

	err := ss.scheduler.Shutdown()
	if err != nil {
		return fmt.Errorf("failed to shutdown scheduler: %w", err)
	}

	logger.Infof("[%s] ✓ Strategy scheduler stopped", ss.exchangeName)
	ss.started = false

	return nil
}

func (ss *StrategyScheduler) List() []gocron.Job {
	return ss.scheduler.Jobs()
}

// GetStats returns scheduler statistics and job information
func (ss *StrategyScheduler) GetStats() map[string]interface{} {
	jobs := ss.scheduler.Jobs()

	stats := map[string]interface{}{
		"total_jobs": len(jobs),
		"running":    true, // scheduler is running if we can call this method
		"jobs":       make([]map[string]interface{}, len(jobs)),
	}

	for i, job := range jobs {
		nextRun, _ := job.NextRun()
		lastRun, _ := job.LastRun()

		stats["jobs"].([]map[string]interface{})[i] = map[string]interface{}{
			"name":     job.Name(),
			"tags":     job.Tags(),
			"last_run": lastRun,
			"next_run": nextRun,
		}
	}

	return stats
}

// updateStrategyNextRun updates the next execution time in database
func (ss *StrategyScheduler) updateStrategyNextRun(strategyID int, nextRun time.Time) error {
	return ss.db.UpdateStrategyNextExecution(strategyID, nextRun)
}
