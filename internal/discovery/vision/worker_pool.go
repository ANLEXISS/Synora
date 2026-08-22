package vision

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"
)

var (
	ErrQueueFull            = errors.New("worker queue full")
	ErrInvalidClipJob       = errors.New("invalid clip job")
	ErrProcessorUnavailable = errors.New("worker processor unavailable")
	ErrPoolClosed           = errors.New("worker pool closed")
)

const defaultMaxAttempts = 2

type WorkerPool struct {
	mu        sync.Mutex
	workersWG sync.WaitGroup
	closed    bool

	process func(*ClipJob) error

	queue chan *ClipJob

	workers int

	maxAttempts int

	activeJobs atomic.Int64
}

func NewWorkerPool(
	workers int,
	process func(*ClipJob) error,
) *WorkerPool {

	p := &WorkerPool{
		process: process,

		queue: make(
			chan *ClipJob,
			128,
		),

		workers: workers,

		maxAttempts: defaultMaxAttempts,
	}
	if p.workers < 1 {
		p.workers = 1
	}

	for i := 0; i < p.workers; i++ {
		p.workersWG.Add(1)

		go p.workerLoop(i)
	}

	return p
}

func (p *WorkerPool) Enqueue(
	job *ClipJob,
) error {
	if p == nil || p.process == nil {
		return ErrProcessorUnavailable
	}
	if job == nil || job.ID == "" || job.CameraID == "" {
		return ErrInvalidClipJob
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPoolClosed
	}

	select {

	case p.queue <- job:

		log.Printf(
			"job queued clip=%s camera=%s",
			job.ID,
			job.CameraID,
		)

		return nil

	default:

		return ErrQueueFull
	}
}

func (p *WorkerPool) workerLoop(
	id int,
) {
	defer p.workersWG.Done()

	log.Printf(
		"worker=%d started",
		id,
	)

	for job := range p.queue {

		p.activeJobs.Add(1)

		log.Printf(
			"worker=%d processing clip=%s camera=%s",
			id,
			job.ID,
			job.CameraID,
		)

		err := p.processWithRetry(job)

		p.activeJobs.Add(-1)

		if err != nil {

			log.Printf(
				"worker=%d failed clip=%s err=%v",
				id,
				job.ID,
				err,
			)

			continue
		}

		log.Printf(
			"worker=%d completed clip=%s",
			id,
			job.ID,
		)
	}
}

// Close stops accepting jobs and waits for already queued jobs to finish.
func (p *WorkerPool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()
	p.workersWG.Wait()
	return nil
}

func (p *WorkerPool) ActiveJobs() int64 {
	if p == nil {
		return 0
	}
	return p.activeJobs.Load()
}

func (p *WorkerPool) processWithRetry(job *ClipJob) error {
	maxAttempts := p.maxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = p.process(job)
		if err == nil {
			return nil
		}
		if attempt < maxAttempts {
			log.Printf("retrying clip=%s attempt=%d/%d err=%v", job.ID, attempt+1, maxAttempts, err)
		}
	}
	return err
}
