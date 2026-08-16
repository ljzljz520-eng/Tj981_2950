package migration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Logger interface {
	Printf(string, ...any)
}

type TargetRepository interface {
	Begin() TargetTransaction
	Snapshot() []TargetLearner
}

type Service struct {
	source SourceRepository
	target TargetRepository
	logger Logger

	mu       sync.RWMutex
	nextTask int
	tasks    map[string]*taskState
}

type taskState struct {
	task       MigrationTask
	sourceIDs  []string
	successful map[string]bool
	failures   map[string]RecordFailure
}

type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}

func NewService(source SourceRepository, target TargetRepository, logger Logger) *Service {
	if logger == nil {
		logger = discardLogger{}
	}
	return &Service{
		source: source,
		target: target,
		logger: logger,
		tasks:  make(map[string]*taskState),
	}
}

func (s *Service) Preview(ctx context.Context) ([]PreviewRecord, error) {
	students, err := s.source.ListStudents(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]PreviewRecord, 0, len(students))
	for _, student := range students {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		learner, err := s.mapStudent(ctx, student)
		if err != nil {
			return nil, err
		}
		preview := PreviewRecord{
			DestinationKey:  learner.RecordKey,
			DisplayName:     learner.DisplayName,
			Identity:        MaskIdentity(learner.GovernmentCredential),
			Program:         learner.Course.Label,
			PermitKind:      learner.Course.PermitKind,
			TheoryMinutes:   learner.Attendance.ClassroomMinutes,
			PracticeMinutes: learner.Attendance.RoadMinutes,
			ExamCount:       len(learner.Assessments),
		}
		result = append(result, preview)
		s.logger.Printf("preview source=%s identity=%s destination=%s", student.LegacyID, preview.Identity, preview.DestinationKey)
	}
	return result, nil
}

func (s *Service) Migrate(ctx context.Context, options RunOptions) (MigrationTask, error) {
	students, err := s.source.ListStudents(ctx)
	if err != nil {
		return MigrationTask{}, err
	}
	studentIDs := make([]string, len(students))
	for i, student := range students {
		studentIDs[i] = student.LegacyID
	}
	state := s.createTask(studentIDs)
	if err := ctx.Err(); err != nil {
		s.setStatus(state, StatusCanceled)
		return s.taskSnapshot(state), err
	}
	return s.runAttempt(ctx, state, studentIDs, options)
}

func (s *Service) Retry(ctx context.Context, taskID string, options RunOptions) (MigrationTask, error) {
	s.mu.Lock()
	state, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return MigrationTask{}, fmt.Errorf("migration task %s not found", taskID)
	}
	if state.task.Status != StatusFailed {
		task := cloneMigrationTask(state.task)
		s.mu.Unlock()
		return task, fmt.Errorf("migration task %s is not retryable", taskID)
	}
	studentIDs := make([]string, 0, len(state.failures))
	for studentID := range state.failures {
		studentIDs = append(studentIDs, studentID)
	}
	sort.Strings(studentIDs)
	state.task.Attempt++
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		s.setStatus(state, StatusCanceled)
		return s.taskSnapshot(state), err
	}
	return s.runAttempt(ctx, state, studentIDs, options)
}

func (s *Service) Task(taskID string) (MigrationTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.tasks[taskID]
	if !ok {
		return MigrationTask{}, fmt.Errorf("migration task %s not found", taskID)
	}
	return cloneMigrationTask(state.task), nil
}

func (s *Service) Confirm(ctx context.Context, taskID string) (ConfirmationChecklist, error) {
	s.mu.RLock()
	state, ok := s.tasks[taskID]
	if !ok {
		s.mu.RUnlock()
		return ConfirmationChecklist{}, fmt.Errorf("migration task %s not found", taskID)
	}
	studentIDs := append([]string(nil), state.sourceIDs...)
	status := state.task.Status
	s.mu.RUnlock()

	targetByOrigin := make(map[string]TargetLearner)
	for _, learner := range s.target.Snapshot() {
		targetByOrigin[learner.OriginKey] = learner
	}
	expectedHours := 0
	actualHours := 0
	expectedExams := 0
	actualExams := 0
	credentialMatches := 0
	programMatches := 0
	actualRecords := 0
	for _, studentID := range studentIDs {
		if err := ctx.Err(); err != nil {
			return ConfirmationChecklist{}, err
		}
		student, err := s.source.Student(ctx, studentID)
		if err != nil {
			return ConfirmationChecklist{}, err
		}
		class, err := s.source.Class(ctx, student.ClassID)
		if err != nil {
			return ConfirmationChecklist{}, err
		}
		hours, err := s.source.Hours(ctx, studentID)
		if err != nil {
			return ConfirmationChecklist{}, err
		}
		exams, err := s.source.Exams(ctx, studentID)
		if err != nil {
			return ConfirmationChecklist{}, err
		}
		expectedHours += hours.TheoryMinutes + hours.PracticeMinutes
		expectedExams += len(exams)
		learner, exists := targetByOrigin[studentID]
		if !exists {
			continue
		}
		actualRecords++
		actualHours += learner.Attendance.ClassroomMinutes + learner.Attendance.RoadMinutes
		actualExams += len(learner.Assessments)
		if learner.GovernmentCredential == student.IdentityNumber {
			credentialMatches++
		}
		if learner.Course.OfferingCode == "offering:"+class.ClassID && learner.Course.PermitKind == class.VehicleCategory {
			programMatches++
		}
	}
	items := []ConfirmationItem{
		confirmationItem("migration-completed", 1, boolInt(status == StatusCompleted)),
		confirmationItem("record-count", len(studentIDs), actualRecords),
		confirmationItem("credential-match-count", len(studentIDs), credentialMatches),
		confirmationItem("program-match-count", len(studentIDs), programMatches),
		confirmationItem("study-minute-total", expectedHours, actualHours),
		confirmationItem("exam-record-count", expectedExams, actualExams),
	}
	checklist := ConfirmationChecklist{TaskID: taskID, AllPassed: true, Items: items}
	for _, item := range items {
		if !item.Passed {
			checklist.AllPassed = false
			break
		}
	}
	s.logger.Printf("confirmation task=%s passed=%t records=%d", taskID, checklist.AllPassed, actualRecords)
	return checklist, nil
}

func (s *Service) createTask(studentIDs []string) *taskState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextTask++
	state := &taskState{
		task: MigrationTask{
			TaskID:  fmt.Sprintf("migration-%03d", s.nextTask),
			Status:  StatusPending,
			Attempt: 1,
			Total:   len(studentIDs),
		},
		sourceIDs:  append([]string(nil), studentIDs...),
		successful: make(map[string]bool),
		failures:   make(map[string]RecordFailure),
	}
	s.tasks[state.task.TaskID] = state
	return state
}

func (s *Service) runAttempt(ctx context.Context, state *taskState, studentIDs []string, options RunOptions) (MigrationTask, error) {
	s.setStatus(state, StatusRunning)
	s.emitProgress(state, "", options.OnProgress)
	tx := s.target.Begin()
	stagedIDs := make([]string, 0, len(studentIDs))
	for _, studentID := range studentIDs {
		student, err := s.source.Student(ctx, studentID)
		if err == nil {
			var learner TargetLearner
			learner, err = s.mapStudent(ctx, student)
			if err == nil {
				err = tx.Stage(learner)
			}
		}
		if err != nil {
			s.recordFailure(state, studentID, err)
		} else {
			s.recordSuccess(state, studentID)
			stagedIDs = append(stagedIDs, studentID)
		}
		event := s.emitProgress(state, studentID, options.OnProgress)
		if options.AfterRecord != nil {
			options.AfterRecord(event)
		}
		if ctx.Err() != nil {
			// The caller signaled cancellation after this record. Roll back the
			// transaction so the target never commits a partial archive, mark the
			// task as canceled, and surface the cancellation so it can be retried
			// safely from a clean target state.
			s.logger.Printf("task=%s cancellation requested after source=%s", event.TaskID, studentID)
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				s.logger.Printf("task=%s rollback error=%v", event.TaskID, rollbackErr)
			}
			s.setStatus(state, StatusCanceled)
			s.emitProgress(state, "", options.OnProgress)
			task := s.taskSnapshot(state)
			s.logger.Printf("task=%s status=%s processed=%d succeeded=%d failed=%d", task.TaskID, task.Status, task.Processed, task.Succeeded, task.Failed)
			return task, ctx.Err()
		}
	}
	if err := tx.Commit(); err != nil {
		for _, studentID := range stagedIDs {
			s.recordFailure(state, studentID, err)
		}
		s.setStatus(state, StatusFailed)
		s.emitProgress(state, "", options.OnProgress)
		return s.taskSnapshot(state), err
	}
	status := StatusCompleted
	if s.failureCount(state) != 0 {
		status = StatusFailed
	}
	s.setStatus(state, status)
	s.emitProgress(state, "", options.OnProgress)
	task := s.taskSnapshot(state)
	s.logger.Printf("task=%s status=%s processed=%d succeeded=%d failed=%d", task.TaskID, task.Status, task.Processed, task.Succeeded, task.Failed)
	return task, nil
}

func (s *Service) mapStudent(ctx context.Context, student SourceStudent) (TargetLearner, error) {
	class, err := s.source.Class(ctx, student.ClassID)
	if err != nil {
		return TargetLearner{}, err
	}
	hours, err := s.source.Hours(ctx, student.LegacyID)
	if err != nil {
		return TargetLearner{}, err
	}
	exams, err := s.source.Exams(ctx, student.LegacyID)
	if err != nil {
		return TargetLearner{}, err
	}
	assessments := make([]TargetAssessment, 0, len(exams))
	for _, exam := range exams {
		outcome := "not-passed"
		if exam.Passed {
			outcome = "passed"
		}
		assessments = append(assessments, TargetAssessment{
			StageCode: strings.ToUpper(exam.Subject),
			TryNumber: exam.Attempt,
			Mark:      exam.Score,
			Outcome:   outcome,
		})
	}
	return TargetLearner{
		RecordKey:            "learner:" + strings.ToLower(student.LegacyID),
		OriginKey:            student.LegacyID,
		DisplayName:          student.Name,
		GovernmentCredential: student.IdentityNumber,
		Course: TargetCourse{
			OfferingCode: "offering:" + class.ClassID,
			Label:        class.Title,
			PermitKind:   class.VehicleCategory,
		},
		Attendance: TargetAttendance{
			ClassroomMinutes: hours.TheoryMinutes,
			RoadMinutes:      hours.PracticeMinutes,
		},
		Assessments: assessments,
	}, nil
}

func (s *Service) recordSuccess(state *taskState, studentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(state.failures, studentID)
	state.successful[studentID] = true
	s.recalculateLocked(state)
}

func (s *Service) recordFailure(state *taskState, studentID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	failure := state.failures[studentID]
	failure.StudentKey = studentID
	failure.Reason = err.Error()
	failure.Attempts++
	state.failures[studentID] = failure
	delete(state.successful, studentID)
	s.recalculateLocked(state)
}

func (s *Service) recalculateLocked(state *taskState) {
	state.task.Succeeded = len(state.successful)
	state.task.Failed = len(state.failures)
	state.task.Processed = state.task.Succeeded + state.task.Failed
	state.task.Failures = state.task.Failures[:0]
	for _, failure := range state.failures {
		state.task.Failures = append(state.task.Failures, failure)
	}
	sort.Slice(state.task.Failures, func(i, j int) bool {
		return state.task.Failures[i].StudentKey < state.task.Failures[j].StudentKey
	})
}

func (s *Service) emitProgress(state *taskState, current string, sink func(ProgressEvent)) ProgressEvent {
	task := s.taskSnapshot(state)
	event := ProgressEvent{
		TaskID:         task.TaskID,
		Status:         task.Status,
		Attempt:        task.Attempt,
		Processed:      task.Processed,
		Total:          task.Total,
		CurrentStudent: current,
	}
	if sink != nil {
		sink(event)
	}
	return event
}

func (s *Service) taskSnapshot(state *taskState) MigrationTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMigrationTask(state.task)
}

func (s *Service) setStatus(state *taskState, status TaskStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.task.Status = status
}

func (s *Service) failureCount(state *taskState) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(state.failures)
}

func cloneMigrationTask(task MigrationTask) MigrationTask {
	copyValue := task
	copyValue.Failures = append([]RecordFailure(nil), task.Failures...)
	return copyValue
}

func confirmationItem(code string, expected, actual int) ConfirmationItem {
	return ConfirmationItem{Code: code, Expected: expected, Actual: actual, Passed: expected == actual}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func IsPlannedWriteFailure(err error) bool {
	return errors.Is(err, ErrPlannedWrite)
}
