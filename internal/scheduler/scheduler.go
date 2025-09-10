package scheduler

import (
	"context"
	"fmt"
	"time"

	"bot/internal/algorithms"
	"bot/internal/core/database"
	"bot/internal/logger"
	"bot/internal/market"

	"github.com/go-co-op/gocron/v2"
)

// StrategyScheduler manages cron-based execution of trading strategies
type StrategyScheduler struct {
	scheduler       gocron.Scheduler
	db              *database.DB
	strategyManager *StrategyManager
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewStrategyScheduler creates a new strategy scheduler
func NewStrategyScheduler(db *database.DB, marketCollector *market.MarketDataCollector, calculator *market.Calculator, algorithmRegistry *algorithms.AlgorithmRegistry, exchange StrategyExchange) (*StrategyScheduler, error) {
	// Create the scheduler with options
	s, err := gocron.NewScheduler(
		gocron.WithLocation(time.UTC),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create strategy manager for orchestrating execution
	strategyManager := NewStrategyManager(db, marketCollector, calculator, algorithmRegistry, exchange)

	return &StrategyScheduler{
		scheduler:       s,
		db:              db,
		strategyManager: strategyManager,
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// Start the scheduler and load all enabled strategies
func (ss *StrategyScheduler) Start() error {
	logger.Info("Starting strategy scheduler...")

	// Load all enabled strategies from database
	strategies, err := ss.db.GetEnabledStrategies()
	if err != nil {
		return fmt.Errorf("failed to load enabled strategies: %w", err)
	}

	logger.Infof("Found %d enabled strategies to schedule", len(strategies))

	// Schedule each strategy
	for _, strategy := range strategies {
		err := ss.ScheduleStrategy(strategy)
		if err != nil {
			logger.Errorf("Failed to schedule strategy %s: %v", strategy.Name, err)
			continue
		}
		logger.Infof("✓ Scheduled strategy '%s' (%s) with cron '%s'",
			strategy.Name, strategy.AlgorithmName, strategy.CronExpression)
	}

	// Start the scheduler
	ss.scheduler.Start()
	logger.Info("✓ Strategy scheduler started successfully")

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

	// Get next execution time
	nextRun, err := job.NextRun()
	if err == nil {
		logger.Infof("Job created for strategy %s (ID: %d), next run: %v", strategy.Name, strategy.ID, nextRun)
	} else {
		logger.Infof("Job created for strategy %s (ID: %d)", strategy.Name, strategy.ID)
	}

	return nil
}

// executeStrategy is called by the scheduler to execute a strategy
func (ss *StrategyScheduler) executeStrategy(strategyID int) {
	logger.Infof("🎯 Executing strategy ID: %d", strategyID)

	// Load strategy from database
	strategy, err := ss.db.GetStrategy(strategyID)
	if err != nil {
		logger.Errorf("Failed to load strategy %d: %v", strategyID, err)
		return
	}

	if !strategy.Enabled {
		logger.Warnf("Strategy %s is disabled, skipping execution", strategy.Name)
		return
	}

	// Execute strategy using StrategyManager
	err = ss.strategyManager.ExecuteStrategy(*strategy)
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

	logger.Infof("✅ Strategy '%s' executed successfully at %v", strategy.Name, now.Format("15:04:05"))
}

// AddStrategy adds a new strategy to the scheduler at runtime
func (ss *StrategyScheduler) AddStrategy(strategy database.Strategy) error {
	logger.Infof("Adding new strategy to scheduler: %s", strategy.Name)
	return ss.ScheduleStrategy(strategy)
}

// RemoveStrategy removes a strategy from the scheduler
func (ss *StrategyScheduler) RemoveStrategy(strategyID int) error {
	// Use tags to find and remove the job
	tag := fmt.Sprintf("strategy-%d", strategyID)
	ss.scheduler.RemoveByTags(tag)

	logger.Infof("Strategy %d removed from scheduler", strategyID)
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

// Stop the scheduler gracefully
func (ss *StrategyScheduler) Stop() error {
	logger.Info("Stopping strategy scheduler...")

	ss.cancel()

	err := ss.scheduler.Shutdown()
	if err != nil {
		return fmt.Errorf("failed to shutdown scheduler: %w", err)
	}

	logger.Info("✓ Strategy scheduler stopped")
	return nil
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
	// TODO: Add proper method to database package for updating next execution time
	// For now, we'll skip this functionality
	logger.Debugf("Would update strategy %d next run time to %v", strategyID, nextRun)
	return nil
}
