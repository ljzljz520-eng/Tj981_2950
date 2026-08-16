package migration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"drivemigrate/fixture"
	"drivemigrate/migration"
)

func TestPreviewMapsAllSourceDataAndRedactsIdentity(t *testing.T) {
	dataset := loadDataset(t)
	source := newSource(t, dataset)
	target := migration.NewMemoryTarget()
	var logs bytes.Buffer
	service := migration.NewService(source, target, log.New(&logs, "", 0))

	preview, err := service.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(preview) != 3 {
		t.Fatalf("len(preview) = %d, want 3", len(preview))
	}
	if target.Count() != 0 {
		t.Errorf("target.Count() = %d, want 0", target.Count())
	}
	first := preview[0]
	if first.DestinationKey != "learner:stu-001" {
		t.Errorf("DestinationKey = %q", first.DestinationKey)
	}
	if first.Identity != "110***********1234" {
		t.Errorf("Identity = %q", first.Identity)
	}
	if first.Program != "Weekday C1 Standard" || first.PermitKind != "C1" {
		t.Errorf("program mapping = %q/%q", first.Program, first.PermitKind)
	}
	if first.TheoryMinutes != 720 || first.PracticeMinutes != 1440 || first.ExamCount != 2 {
		t.Errorf("activity mapping = %+v", first)
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	combined := string(encoded) + logs.String()
	for _, student := range dataset.Students {
		if strings.Contains(combined, student.IdentityNumber) {
			t.Errorf("output contains identity for %s", student.LegacyID)
		}
	}
}

func TestMigrationProgressRetryAndConfirmation(t *testing.T) {
	dataset := loadDataset(t)
	source := newSource(t, dataset)
	target := migration.NewMemoryTarget()
	service := migration.NewService(source, target, nil)
	target.FailNext("STU-002", 1)
	events := make([]migration.ProgressEvent, 0, 8)
	options := migration.RunOptions{OnProgress: func(event migration.ProgressEvent) {
		events = append(events, event)
	}}

	initial, err := service.Migrate(context.Background(), options)
	if err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if initial.Status != migration.StatusFailed {
		t.Errorf("initial.Status = %q, want %q", initial.Status, migration.StatusFailed)
	}
	if initial.Processed != 3 || initial.Succeeded != 2 || initial.Failed != 1 {
		t.Errorf("initial counts = %+v", initial)
	}
	if len(initial.Failures) != 1 || initial.Failures[0].StudentKey != "STU-002" || initial.Failures[0].Attempts != 1 {
		t.Errorf("initial.Failures = %+v", initial.Failures)
	}
	if !migration.IsPlannedWriteFailure(errors.New(initial.Failures[0].Reason)) && !strings.Contains(initial.Failures[0].Reason, migration.ErrPlannedWrite.Error()) {
		t.Errorf("failure reason = %q", initial.Failures[0].Reason)
	}
	if target.Count() != 2 {
		t.Errorf("target.Count() = %d, want 2", target.Count())
	}

	final, err := service.Retry(context.Background(), initial.TaskID, options)
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if final.Status != migration.StatusCompleted || final.Attempt != 2 {
		t.Errorf("final state = %+v", final)
	}
	if final.Processed != 3 || final.Succeeded != 3 || final.Failed != 0 || len(final.Failures) != 0 {
		t.Errorf("final counts = %+v", final)
	}
	if target.Count() != 3 {
		t.Errorf("target.Count() = %d, want 3", target.Count())
	}
	if len(events) != 8 {
		t.Fatalf("len(events) = %d, want 8", len(events))
	}
	wantCurrent := []string{"", "STU-001", "STU-002", "STU-003", "", "", "STU-002", ""}
	for i, current := range wantCurrent {
		if events[i].CurrentStudent != current {
			t.Errorf("events[%d].CurrentStudent = %q, want %q", i, events[i].CurrentStudent, current)
		}
	}
	if events[0].Status != migration.StatusRunning || events[4].Status != migration.StatusFailed || events[7].Status != migration.StatusCompleted {
		t.Errorf("progress statuses = %+v", events)
	}

	checklist, err := service.Confirm(context.Background(), final.TaskID)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if !checklist.AllPassed || len(checklist.Items) != 6 {
		t.Fatalf("checklist = %+v", checklist)
	}
	for _, item := range checklist.Items {
		if !item.Passed {
			t.Errorf("checklist item = %+v", item)
		}
	}
}

func TestMigrationCancellationIsAtomicAndRetryable(t *testing.T) {
	dataset := loadDataset(t)
	source := newSource(t, dataset)
	target := migration.NewMemoryTarget()
	service := migration.NewService(source, target, nil)
	ctx, cancel := context.WithCancel(context.Background())

	task, err := service.Migrate(ctx, migration.RunOptions{AfterRecord: func(event migration.ProgressEvent) {
		if event.Processed == 1 {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Migrate() error = %v, want context canceled", err)
	}
	if task.Status != migration.StatusCanceled {
		t.Errorf("task.Status = %q, want %q", task.Status, migration.StatusCanceled)
	}
	if target.Count() != 0 {
		t.Errorf("target.Count() = %d, want 0", target.Count())
	}

	retried, retryErr := service.Migrate(context.Background(), migration.RunOptions{})
	if retryErr != nil {
		t.Fatalf("retry Migrate() error = %v", retryErr)
	}
	if retried.Status != migration.StatusCompleted || target.Count() != 3 {
		t.Errorf("retry result = %+v, target count = %d", retried, target.Count())
	}
}

func TestTargetTransactionVisibilityUsesChannelBarrier(t *testing.T) {
	target := migration.NewMemoryTarget()
	tx := target.Begin()
	for _, id := range []string{"STU-001", "STU-002", "STU-003"} {
		err := tx.Stage(migration.TargetLearner{RecordKey: "learner:" + strings.ToLower(id), OriginKey: id})
		if err != nil {
			t.Fatalf("Stage() error = %v", err)
		}
	}
	ready := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(ready)
		<-release
		done <- tx.Commit()
	}()
	<-ready
	if target.Count() != 0 {
		t.Errorf("target.Count() before commit = %d", target.Count())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if target.Count() != 3 {
		t.Errorf("target.Count() after commit = %d", target.Count())
	}
}

func loadDataset(t *testing.T) migration.SourceDataset {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	dataset, err := fixture.Load(filepath.Join(filepath.Dir(filename), "..", "testdata", "source.yaml"))
	if err != nil {
		t.Fatalf("fixture.Load() error = %v", err)
	}
	return dataset
}

func newSource(t *testing.T, dataset migration.SourceDataset) *migration.MemorySource {
	t.Helper()
	source, err := migration.NewMemorySource(dataset)
	if err != nil {
		t.Fatalf("migration.NewMemorySource() error = %v", err)
	}
	return source
}
