package jobs

import (
	"context"
	"errors"
	"sync"
)

type Job func(context.Context) error

type Queue struct {
	jobs        chan Job
	workerCount int
	wg          sync.WaitGroup
}

func NewQueue(workerCount int, capacity int) (*Queue, error) {
	if workerCount <= 0 {
		return nil, errors.New("worker count must be positive")
	}
	if capacity <= 0 {
		capacity = workerCount
	}
	return &Queue{jobs: make(chan Job, capacity), workerCount: workerCount}, nil
}

func (q *Queue) Start(ctx context.Context, onError func(error)) {
	for range q.workerCount {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-q.jobs:
					if !ok {
						return
					}
					if err := job(ctx); err != nil && onError != nil {
						onError(err)
					}
				}
			}
		}()
	}
}

func (q *Queue) Submit(ctx context.Context, job Job) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case q.jobs <- job:
		return nil
	}
}

func (q *Queue) Stop() {
	close(q.jobs)
	q.wg.Wait()
}
