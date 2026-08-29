package vision

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"synora/internal/resourcebudget"
)

var (
	ErrQueueFull            = errors.New("worker queue full")
	ErrInvalidClipJob       = errors.New("invalid clip job")
	ErrProcessorUnavailable = errors.New("worker processor unavailable")
	ErrPoolClosed           = errors.New("worker pool closed")
	ErrProcessingTimeout    = errors.New("clip processing timeout")
	ErrQueuePersistence     = errors.New("worker queue persistence failed")
)

const (
	defaultMaxAttempts     = 2
	defaultQueueSize       = resourcebudget.MaxVisionQueue / 4
	defaultProcessTimeout  = WorkerTimeout
	defaultRetryBackoff    = 100 * time.Millisecond
	defaultMaxRetryBackoff = 2 * time.Second
)

// WorkerPoolConfig controls the durable delivery boundary between Discovery
// and the Vision runtime. PersistencePath is optional for embedded callers;
// the production manager supplies a path under the clip root.
type WorkerPoolConfig struct {
	QueueSize          int
	MaxAttempts        int
	ProcessTimeout     time.Duration
	RetryBackoff       time.Duration
	MaxRetryBackoff    time.Duration
	PersistencePath    string
	OnPermanentFailure func(*ClipJob, error)
}

type WorkerPool struct {
	mu        sync.Mutex
	workersWG sync.WaitGroup
	closed    bool

	process func(*ClipJob) error

	queue chan *ClipJob

	workers         int
	queueSize       int
	maxAttempts     int
	processTimeout  time.Duration
	retryBackoff    time.Duration
	maxRetryBackoff time.Duration
	persistencePath string
	onPermanentFail func(*ClipJob, error)
	pending         map[string]*ClipJob
	pendingOrder    []string

	activeJobs atomic.Int64
}

func NewWorkerPool(workers int, process func(*ClipJob) error) *WorkerPool {
	return NewWorkerPoolWithConfig(workers, process, WorkerPoolConfig{})
}

func NewWorkerPoolWithConfig(workers int, process func(*ClipJob) error, cfg WorkerPoolConfig) *WorkerPool {
	if workers < 1 {
		workers = 1
	}
	if workers > resourcebudget.MaxVisionWorkers {
		workers = resourcebudget.MaxVisionWorkers
	}
	if cfg.QueueSize < 1 {
		cfg.QueueSize = defaultQueueSize
	}
	if cfg.QueueSize > resourcebudget.MaxVisionQueue {
		cfg.QueueSize = resourcebudget.MaxVisionQueue
	}
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.ProcessTimeout <= 0 {
		cfg.ProcessTimeout = defaultProcessTimeout
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = defaultRetryBackoff
	}
	if cfg.MaxRetryBackoff <= 0 {
		cfg.MaxRetryBackoff = defaultMaxRetryBackoff
	}
	persistencePath := strings.TrimSpace(cfg.PersistencePath)
	if persistencePath != "" {
		persistencePath = filepath.Clean(persistencePath)
	}
	p := &WorkerPool{
		process:         process,
		queue:           make(chan *ClipJob, cfg.QueueSize),
		workers:         workers,
		queueSize:       cfg.QueueSize,
		maxAttempts:     cfg.MaxAttempts,
		processTimeout:  cfg.ProcessTimeout,
		retryBackoff:    cfg.RetryBackoff,
		maxRetryBackoff: cfg.MaxRetryBackoff,
		persistencePath: persistencePath,
		onPermanentFail: cfg.OnPermanentFailure,
		pending:         map[string]*ClipJob{},
	}
	p.restore()
	for i := 0; i < p.workers; i++ {
		p.workersWG.Add(1)
		go p.workerLoop(i)
	}
	return p
}

func (p *WorkerPool) Enqueue(job *ClipJob) error {
	if p == nil || p.process == nil {
		return ErrProcessorUnavailable
	}
	if !validClipJob(job) {
		return ErrInvalidClipJob
	}
	cloned := cloneClipJob(job)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPoolClosed
	}
	if _, exists := p.pending[cloned.ID]; exists {
		return nil
	}
	if len(p.pending) >= p.queueSize {
		return ErrQueueFull
	}
	p.pending[cloned.ID] = cloned
	p.pendingOrder = append(p.pendingOrder, cloned.ID)
	if err := p.persistLocked(); err != nil {
		delete(p.pending, cloned.ID)
		p.pendingOrder = p.pendingOrder[:len(p.pendingOrder)-1]
		return err
	}
	select {
	case p.queue <- cloned:
		log.Printf("job queued clip=%s camera=%s", cloned.ID, cloned.CameraID)
		return nil
	default:
		delete(p.pending, cloned.ID)
		p.pendingOrder = p.pendingOrder[:len(p.pendingOrder)-1]
		_ = p.persistLocked()
		return ErrQueueFull
	}
}

func (p *WorkerPool) workerLoop(id int) {
	defer p.workersWG.Done()
	log.Printf("worker=%d started", id)
	for job := range p.queue {
		p.activeJobs.Add(1)
		log.Printf("worker=%d processing clip=%s camera=%s", id, job.ID, job.CameraID)
		err := p.processWithRetry(job)
		p.activeJobs.Add(-1)
		if err != nil {
			log.Printf("worker=%d failed clip=%s err=%v", id, job.ID, err)
			if p.onPermanentFail != nil {
				p.onPermanentFail(job, err)
			}
		}
		p.complete(job.ID)
		if err == nil {
			log.Printf("worker=%d completed clip=%s", id, job.ID)
		}
	}
}

// Close stops accepting jobs and waits for already queued jobs to finish.
// Jobs remain in the durable journal until their processing callback returns
// successfully or the permanent-failure callback has been invoked.
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
	var err error
	for attempt := 1; attempt <= p.maxAttempts; attempt++ {
		err = p.processOnce(job)
		if err == nil {
			return nil
		}
		if attempt == p.maxAttempts {
			break
		}
		backoff := p.retryBackoff
		if backoff > p.maxRetryBackoff {
			backoff = p.maxRetryBackoff
		}
		for retry := 1; retry < attempt; retry++ {
			if backoff >= p.maxRetryBackoff/2 {
				backoff = p.maxRetryBackoff
				break
			}
			backoff *= 2
		}
		log.Printf("retrying clip=%s attempt=%d/%d err=%v", job.ID, attempt+1, p.maxAttempts, err)
		timer := time.NewTimer(backoff)
		<-timer.C
	}
	return err
}

func (p *WorkerPool) processOnce(job *ClipJob) error {
	if p.processTimeout <= 0 {
		return p.process(job)
	}
	result := make(chan error, 1)
	go func() { result <- p.process(job) }()
	timer := time.NewTimer(p.processTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return ErrProcessingTimeout
	}
}

func (p *WorkerPool) complete(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	job, exists := p.pending[id]
	if !exists {
		return
	}
	delete(p.pending, id)
	for index, pendingID := range p.pendingOrder {
		if pendingID == id {
			p.pendingOrder = append(p.pendingOrder[:index], p.pendingOrder[index+1:]...)
			break
		}
	}
	if err := p.persistLocked(); err != nil {
		p.pending[id] = job
		p.pendingOrder = append(p.pendingOrder, id)
		log.Printf("worker queue completion persistence failed clip=%s err=%v", id, err)
	}
}

func (p *WorkerPool) restore() {
	if p.persistencePath == "" {
		return
	}
	data, err := os.ReadFile(p.persistencePath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("worker queue restore failed err=%v", err)
		return
	}
	var jobs []*ClipJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		log.Printf("worker queue restore ignored malformed journal err=%v", err)
		return
	}
	for _, job := range jobs {
		if !validClipJob(job) || len(p.pending) >= p.queueSize {
			continue
		}
		cloned := cloneClipJob(job)
		if _, exists := p.pending[cloned.ID]; exists {
			continue
		}
		p.pending[cloned.ID] = cloned
		p.pendingOrder = append(p.pendingOrder, cloned.ID)
		p.queue <- cloned
	}
}

func (p *WorkerPool) persistLocked() error {
	if p.persistencePath == "" {
		return nil
	}
	jobs := make([]*ClipJob, 0, len(p.pendingOrder))
	for _, id := range p.pendingOrder {
		if job := p.pending[id]; job != nil {
			jobs = append(jobs, cloneClipJob(job))
		}
	}
	data, err := json.Marshal(jobs)
	if err != nil {
		return fmt.Errorf("%w: marshal: %v", ErrQueuePersistence, err)
	}
	directory := filepath.Dir(p.persistencePath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("%w: mkdir: %v", ErrQueuePersistence, err)
	}
	temp, err := os.CreateTemp(directory, ".vision-queue-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create: %v", ErrQueuePersistence, err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0600); err != nil {
		return fmt.Errorf("%w: chmod: %v", ErrQueuePersistence, err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("%w: write: %v", ErrQueuePersistence, err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("%w: sync: %v", ErrQueuePersistence, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("%w: close: %v", ErrQueuePersistence, err)
	}
	if err := os.Rename(tempPath, p.persistencePath); err != nil {
		return fmt.Errorf("%w: rename: %v", ErrQueuePersistence, err)
	}
	keep = true
	return syncQueueDirectory(directory)
}

func syncQueueDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open directory: %v", ErrQueuePersistence, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("%w: sync directory: %v", ErrQueuePersistence, err)
	}
	return nil
}

func validClipJob(job *ClipJob) bool {
	return job != nil && strings.TrimSpace(job.ID) != "" && strings.TrimSpace(job.CameraID) != ""
}

func cloneClipJob(job *ClipJob) *ClipJob {
	if job == nil {
		return nil
	}
	cloned := *job
	return &cloned
}
