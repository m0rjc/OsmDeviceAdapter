package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreoutbox"
)

// OutboxProcessorConfig holds configuration for the outbox processor
type OutboxProcessorConfig struct {
	// PollInterval is how often to check for pending entries
	PollInterval time.Duration

	// WorkerCount is the number of concurrent workers (default: 1)
	// Per design doc: single worker to reduce OSM API load
	WorkerCount int
}

// DefaultConfig returns the default configuration as specified in the planning doc
func DefaultConfig() OutboxProcessorConfig {
	return OutboxProcessorConfig{
		PollInterval: 30 * time.Second, // 30-second poll interval
		WorkerCount:  1,                 // Single worker (not concurrent)
	}
}

// OutboxProcessor is the background worker that processes pending outbox entries.
// It polls the database for pending entries and delegates to PatrolSyncService.
type OutboxProcessor struct {
	config      OutboxProcessorConfig
	conns       *db.Connections
	syncService *PatrolSyncService
	stopChan    chan struct{}
	wg          sync.WaitGroup
	running     bool
	mu          sync.Mutex
}

// NewOutboxProcessor creates a new outbox processor
func NewOutboxProcessor(config OutboxProcessorConfig, conns *db.Connections, syncService *PatrolSyncService) *OutboxProcessor {
	return &OutboxProcessor{
		config:      config,
		conns:       conns,
		syncService: syncService,
		stopChan:    make(chan struct{}),
	}
}

// Start starts the background worker goroutines
func (p *OutboxProcessor) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return nil // Already running
	}

	p.running = true
	p.stopChan = make(chan struct{})

	// Start worker goroutines
	for i := 0; i < p.config.WorkerCount; i++ {
		p.wg.Add(1)
		go p.workerLoop(ctx, i)
	}

	slog.Info("worker.outbox_processor.started",
		"component", "worker.outbox_processor",
		"event", "processor.started",
		"worker_count", p.config.WorkerCount,
		"poll_interval", p.config.PollInterval,
	)

	return nil
}

// Stop gracefully stops the background worker
func (p *OutboxProcessor) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return // Not running
	}

	slog.Info("worker.outbox_processor.stopping",
		"component", "worker.outbox_processor",
		"event", "processor.stopping",
	)

	close(p.stopChan)
	p.running = false

	// Wait for all workers to finish (with timeout)
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("worker.outbox_processor.stopped",
			"component", "worker.outbox_processor",
			"event", "processor.stopped",
		)
	case <-time.After(30 * time.Second):
		slog.Warn("worker.outbox_processor.stop_timeout",
			"component", "worker.outbox_processor",
			"event", "processor.stop_timeout",
		)
	}
}

// workerLoop is the main worker loop that polls for pending entries
func (p *OutboxProcessor) workerLoop(ctx context.Context, workerID int) {
	defer p.wg.Done()

	logger := slog.With(
		"component", "worker.outbox_processor",
		"worker_id", workerID,
	)

	logger.Info("worker.outbox_processor.worker_started",
		"event", "worker.started",
	)

	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	// Process immediately on startup
	p.processPendingEntries(ctx, logger)

	for {
		select {
		case <-p.stopChan:
			logger.Info("worker.outbox_processor.worker_stopped",
				"event", "worker.stopped",
			)
			return

		case <-ticker.C:
			p.processPendingEntries(ctx, logger)

		case <-ctx.Done():
			logger.Info("worker.outbox_processor.worker_context_cancelled",
				"event", "worker.cancelled",
			)
			return
		}
	}
}

// processPendingEntries finds all pending user+patrol combinations and processes them
func (p *OutboxProcessor) processPendingEntries(ctx context.Context, logger *slog.Logger) {
	// Find all user+section+patrol tuples with pending entries
	userPatrols, err := scoreoutbox.FindUserPatrolsWithPending(p.conns)
	if err != nil {
		logger.Error("worker.outbox_processor.find_pending_failed",
			"event", "processor.find_error",
			"error", err,
		)
		return
	}

	if len(userPatrols) == 0 {
		// No pending entries - this is normal
		logger.Debug("worker.outbox_processor.no_pending",
			"event", "processor.no_pending",
		)
		return
	}

	logger.Info("worker.outbox_processor.processing_patrols",
		"event", "processor.processing",
		"patrol_count", len(userPatrols),
	)

	// Process each user+patrol combination
	successCount := 0
	errorCount := 0

	for _, up := range userPatrols {
		// Use a per-patrol timeout to prevent indefinite blocking
		patrolCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)

		err := p.syncService.SyncPatrol(patrolCtx, up.OSMUserID, up.SectionID, up.PatrolID)
		cancel()

		if err != nil {
			errorCount++
			logger.Error("worker.outbox_processor.sync_failed",
				"event", "processor.sync_error",
				"osm_user_id", up.OSMUserID,
				"section_id", up.SectionID,
				"patrol_id", up.PatrolID,
				"error", err,
			)
			// Continue to next patrol - don't let one failure stop others
		} else {
			successCount++
		}
	}

	logger.Info("worker.outbox_processor.batch_complete",
		"event", "processor.batch_complete",
		"total_patrols", len(userPatrols),
		"success_count", successCount,
		"error_count", errorCount,
	)
}
