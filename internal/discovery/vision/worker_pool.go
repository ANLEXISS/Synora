package vision

import (
	"errors"
	"log"
	"sync/atomic"
)

var ErrQueueFull = errors.New(
	"worker queue full",
)

type WorkerPool struct {
	process func(*ClipJob) error

	queue chan *ClipJob

	workers int

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

		err := p.process(
			job,
		)

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
