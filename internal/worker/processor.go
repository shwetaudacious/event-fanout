package worker

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/queue"
	"github.com/event-fanout-service/event-fanout/internal/service"
)

// Processor consumes Redis Stream events and processes pending retries.
type Processor struct {
	queue          queue.Queue
	fanout         *service.FanoutService
	pollInterval   time.Duration
	readTimeout    time.Duration
	retryBatchSize int
	logger         *zap.Logger
}

// NewProcessor creates a background event processor.
func NewProcessor(
	q queue.Queue,
	fanout *service.FanoutService,
	pollInterval time.Duration,
	readTimeout time.Duration,
	retryBatchSize int,
	logger *zap.Logger,
) *Processor {
	return &Processor{
		queue:          q,
		fanout:         fanout,
		pollInterval:   pollInterval,
		readTimeout:    readTimeout,
		retryBatchSize: retryBatchSize,
		logger:         logger,
	}
}

// Run starts the worker loop until the context is cancelled.
func (p *Processor) Run(ctx context.Context) {
	if err := p.queue.InitConsumerGroup(ctx); err != nil {
		p.logger.Error("failed to init consumer group", zap.Error(err))
		return
	}

	p.reclaimAndProcess(ctx)

	retryTicker := time.NewTicker(p.pollInterval)
	defer retryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("worker shutting down")
			return
		default:
		}

		msg, err := p.queue.ReadEvent(ctx, p.readTimeout)
		if err != nil {
			p.logger.Warn("stream read failed", zap.Error(err))
			continue
		}
		if msg != nil {
			p.processMessage(ctx, msg)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-retryTicker.C:
			p.reclaimAndProcess(ctx)
			if err := p.fanout.ProcessPendingRetries(ctx, p.retryBatchSize); err != nil {
				p.logger.Warn("retry processing failed", zap.Error(err))
			}
		}
	}
}

func (p *Processor) reclaimAndProcess(ctx context.Context) {
	messages, err := p.queue.ReclaimPending(ctx, 30*time.Second, int64(p.retryBatchSize))
	if err != nil {
		p.logger.Warn("reclaim pending failed", zap.Error(err))
		return
	}
	for i := range messages {
		p.processMessage(ctx, &messages[i])
	}
}

func (p *Processor) processMessage(ctx context.Context, msg *queue.EventMessage) {
	if err := p.fanout.ProcessEvent(ctx, msg.Event); err != nil {
		p.logger.Error("process event failed",
			zap.String("event_id", msg.Event.ID.String()),
			zap.String("stream_id", msg.StreamID),
			zap.Error(err),
		)
		return
	}
	if err := p.queue.AckEvent(ctx, msg.StreamID); err != nil {
		p.logger.Warn("ack failed",
			zap.String("stream_id", msg.StreamID),
			zap.Error(err),
		)
	}
}
