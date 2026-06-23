package worker

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/service"
)

// Processor consumes queued events and processes pending retries.
type Processor struct {
	queue          queue.Queue
	fanout         *service.FanoutService
	pollInterval   time.Duration
	dequeueTimeout time.Duration
	retryBatchSize int
	logger         *zap.Logger
}

// NewProcessor creates a background event processor.
func NewProcessor(
	q queue.Queue,
	fanout *service.FanoutService,
	pollInterval time.Duration,
	dequeueTimeout time.Duration,
	retryBatchSize int,
	logger *zap.Logger,
) *Processor {
	return &Processor{
		queue:          q,
		fanout:         fanout,
		pollInterval:   pollInterval,
		dequeueTimeout: dequeueTimeout,
		retryBatchSize: retryBatchSize,
		logger:         logger,
	}
}

// Run starts the worker loop until the context is cancelled.
func (p *Processor) Run(ctx context.Context) {
	retryTicker := time.NewTicker(p.pollInterval)
	defer retryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("worker shutting down")
			return
		default:
		}

		event, err := p.queue.DequeueEvent(ctx, p.dequeueTimeout)
		if err != nil {
			p.logger.Warn("dequeue failed", zap.Error(err))
			continue
		}
		if event != nil {
			if err := p.fanout.ProcessEvent(ctx, event); err != nil {
				p.logger.Error("process event failed",
					zap.String("event_id", event.ID.String()),
					zap.Error(err),
				)
			}
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-retryTicker.C:
			if err := p.fanout.ProcessPendingRetries(ctx, p.retryBatchSize); err != nil {
				p.logger.Warn("retry processing failed", zap.Error(err))
			}
		}
	}
}
