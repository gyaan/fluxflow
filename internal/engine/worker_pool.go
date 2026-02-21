package engine

import (
	"context"
	"sync"
)

// job is the unit of work dispatched to a worker.
type job[T any] struct {
	payload T
	result  chan<- jobResult[T]
}

type jobResult[T any] struct {
	payload T
	err     error
}

// workerPool is a fixed-size goroutine pool with a bounded input queue.
type workerPool[T, R any] struct {
	queue   chan job[T]
	process func(ctx context.Context, t T) (R, error)
	wg      sync.WaitGroup
}

// newWorkerPool creates and starts a pool with n goroutines and queue capacity cap.
func newWorkerPool[T, R any](ctx context.Context, n, cap int, fn func(context.Context, T) (R, error)) *workerPool[T, R] {
	p := &workerPool[T, R]{
		queue:   make(chan job[T], cap),
		process: fn,
	}
	for i := 0; i < n; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.run(ctx)
		}()
	}
	return p
}

func (p *workerPool[T, R]) run(ctx context.Context) {
	for {
		select {
		case j, ok := <-p.queue:
			if !ok {
				return
			}
			_, err := p.process(ctx, j.payload)
			if j.result != nil {
				j.result <- jobResult[T]{payload: j.payload, err: err}
			}
		case <-ctx.Done():
			return
		}
	}
}

// Submit enqueues a job without blocking (returns false if full).
func (p *workerPool[T, R]) Submit(t T) bool {
	select {
	case p.queue <- job[T]{payload: t}:
		return true
	default:
		return false
	}
}

// Drain closes the queue and waits for all workers to finish.
func (p *workerPool[T, R]) Drain() {
	close(p.queue)
	p.wg.Wait()
}

// QueueLen returns how many jobs are currently queued.
func (p *workerPool[T, R]) QueueLen() int {
	return len(p.queue)
}

// QueueCap returns the total queue capacity.
func (p *workerPool[T, R]) QueueCap() int {
	return cap(p.queue)
}
