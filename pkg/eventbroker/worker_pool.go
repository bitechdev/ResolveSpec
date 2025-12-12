package eventbroker

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// workerPool manages a pool of workers for async event processing
type workerPool struct {
	workerCount int
	bufferSize  int
	eventQueue  chan *Event
	processor   func(context.Context, *Event) error

	activeWorkers atomic.Int32
	isRunning     atomic.Bool
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// newWorkerPool creates a new worker pool
func newWorkerPool(workerCount, bufferSize int, processor func(context.Context, *Event) error) *workerPool {
	return &workerPool{
		workerCount: workerCount,
		bufferSize:  bufferSize,
		eventQueue:  make(chan *Event, bufferSize),
		processor:   processor,
		stopCh:      make(chan struct{}),
	}
}

// Start starts the worker pool
func (wp *workerPool) Start() {
	if wp.isRunning.Load() {
		return
	}

	wp.isRunning.Store(true)

	// Start workers
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}

	logger.Info("Worker pool started with %d workers", wp.workerCount)
}

// Stop stops the worker pool gracefully
func (wp *workerPool) Stop(ctx context.Context) error {
	if !wp.isRunning.Load() {
		return nil
	}

	wp.isRunning.Store(false)

	// Close event queue to signal workers
	close(wp.eventQueue)

	// Wait for workers to finish with context timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("Worker pool stopped gracefully")
		return nil
	case <-ctx.Done():
		logger.Warn("Worker pool stop timed out, some events may be lost")
		return ctx.Err()
	}
}

// Submit submits an event to the queue
func (wp *workerPool) Submit(ctx context.Context, event *Event) error {
	if !wp.isRunning.Load() {
		return ErrWorkerPoolStopped
	}

	select {
	case wp.eventQueue <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrQueueFull
	}
}

// worker is a worker goroutine that processes events from the queue
func (wp *workerPool) worker(id int) {
	defer wp.wg.Done()

	logger.Debug("Worker %d started", id)

	for event := range wp.eventQueue {
		wp.activeWorkers.Add(1)

		// Process event with background context (detached from original request)
		ctx := context.Background()
		if err := wp.processor(ctx, event); err != nil {
			logger.Error("Worker %d failed to process event %s: %v", id, event.ID, err)
		}

		wp.activeWorkers.Add(-1)
	}

	logger.Debug("Worker %d stopped", id)
}

// QueueSize returns the current queue size
func (wp *workerPool) QueueSize() int {
	return len(wp.eventQueue)
}

// ActiveWorkers returns the number of currently active workers
func (wp *workerPool) ActiveWorkers() int {
	return int(wp.activeWorkers.Load())
}

// Error definitions
var (
	ErrWorkerPoolStopped = &BrokerError{Code: "worker_pool_stopped", Message: "worker pool is stopped"}
	ErrQueueFull         = &BrokerError{Code: "queue_full", Message: "event queue is full"}
)

// BrokerError represents an error from the event broker
type BrokerError struct {
	Code    string
	Message string
}

func (e *BrokerError) Error() string {
	return e.Message
}
