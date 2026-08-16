package migration

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var ErrPlannedWrite = errors.New("planned target write failure")

type TargetTransaction interface {
	Stage(TargetLearner) error
	Commit() error
	Rollback() error
}

type MemoryTarget struct {
	mu            sync.RWMutex
	records       map[string]TargetLearner
	failRemaining map[string]int
}

func NewMemoryTarget() *MemoryTarget {
	return &MemoryTarget{
		records:       make(map[string]TargetLearner),
		failRemaining: make(map[string]int),
	}
}

func (r *MemoryTarget) FailNext(studentID string, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if count <= 0 {
		delete(r.failRemaining, studentID)
		return
	}
	r.failRemaining[studentID] = count
}

func (r *MemoryTarget) Begin() TargetTransaction {
	return &memoryTargetTransaction{
		owner:  r,
		staged: make(map[string]TargetLearner),
	}
}

func (r *MemoryTarget) Snapshot() []TargetLearner {
	r.mu.RLock()
	defer r.mu.RUnlock()
	records := make([]TargetLearner, 0, len(r.records))
	for _, learner := range r.records {
		records = append(records, cloneTargetLearner(learner))
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].OriginKey < records[j].OriginKey
	})
	return records
}

func (r *MemoryTarget) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.records)
}

func (r *MemoryTarget) consumeFailure(studentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	remaining := r.failRemaining[studentID]
	if remaining <= 0 {
		return false
	}
	remaining--
	if remaining == 0 {
		delete(r.failRemaining, studentID)
	} else {
		r.failRemaining[studentID] = remaining
	}
	return true
}

type memoryTargetTransaction struct {
	mu     sync.Mutex
	owner  *MemoryTarget
	staged map[string]TargetLearner
	closed bool
}

func (t *memoryTargetTransaction) Stage(learner TargetLearner) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("target transaction is closed")
	}
	if learner.RecordKey == "" || learner.OriginKey == "" {
		return errors.New("target learner has an empty key")
	}
	if t.owner.consumeFailure(learner.OriginKey) {
		return fmt.Errorf("%w for %s", ErrPlannedWrite, learner.OriginKey)
	}
	t.staged[learner.RecordKey] = cloneTargetLearner(learner)
	return nil
}

func (t *memoryTargetTransaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("target transaction is closed")
	}
	t.owner.mu.Lock()
	for key, learner := range t.staged {
		t.owner.records[key] = cloneTargetLearner(learner)
	}
	t.owner.mu.Unlock()
	t.closed = true
	return nil
}

func (t *memoryTargetTransaction) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("target transaction is closed")
	}
	t.closed = true
	t.staged = nil
	return nil
}

func cloneTargetLearner(learner TargetLearner) TargetLearner {
	copyValue := learner
	copyValue.Assessments = append([]TargetAssessment(nil), learner.Assessments...)
	return copyValue
}
