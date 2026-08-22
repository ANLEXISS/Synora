package vision

import (
	"errors"
	"log"
	"sync/atomic"
)

var ErrQueueFull = errors.New(
	"worker queue full",
)

const defaultMaxAttempts = 2

type WorkerPool struct {
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

	for i := 0; i < workers; i++ {

		go p.workerLoop(i)
	}

	return p
}

func (p *WorkerPool) Enqueue(
	job *ClipJob,
) error {

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
