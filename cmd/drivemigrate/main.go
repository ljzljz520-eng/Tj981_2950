package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"drivemigrate/fixture"
	"drivemigrate/migration"
)

type demoOutput struct {
	Preview      []migration.PreviewRecord       `json:"preview"`
	InitialTask  migration.MigrationTask         `json:"initial_task"`
	FinalTask    migration.MigrationTask         `json:"final_task"`
	Progress     []migration.ProgressEvent       `json:"progress"`
	Confirmation migration.ConfirmationChecklist `json:"confirmation"`
}

func main() {
	mode := flag.String("mode", "demo", "preview, migrate, or demo")
	fixturePath := flag.String("fixture", "", "source YAML fixture; embedded data is used when empty")
	flag.Parse()
	if err := run(*mode, *fixturePath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(mode, fixturePath string) error {
	dataset, err := loadFixture(fixturePath)
	if err != nil {
		return err
	}
	source, err := migration.NewMemorySource(dataset)
	if err != nil {
		return err
	}
	target := migration.NewMemoryTarget()
	service := migration.NewService(source, target, log.New(os.Stderr, "", 0))
	ctx := context.Background()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	switch mode {
	case "preview":
		preview, previewErr := service.Preview(ctx)
		if previewErr != nil {
			return previewErr
		}
		return encoder.Encode(preview)
	case "migrate":
		task, migrateErr := service.Migrate(ctx, migration.RunOptions{})
		if migrateErr != nil {
			return migrateErr
		}
		checklist, confirmErr := service.Confirm(ctx, task.TaskID)
		if confirmErr != nil {
			return confirmErr
		}
		return encoder.Encode(struct {
			Task         migration.MigrationTask         `json:"task"`
			Confirmation migration.ConfirmationChecklist `json:"confirmation"`
		}{Task: task, Confirmation: checklist})
	case "demo":
		preview, previewErr := service.Preview(ctx)
		if previewErr != nil {
			return previewErr
		}
		target.FailNext("STU-002", 1)
		progress := make([]migration.ProgressEvent, 0, 8)
		options := migration.RunOptions{OnProgress: func(event migration.ProgressEvent) {
			progress = append(progress, event)
		}}
		initial, migrateErr := service.Migrate(ctx, options)
		if migrateErr != nil {
			return migrateErr
		}
		final, retryErr := service.Retry(ctx, initial.TaskID, options)
		if retryErr != nil {
			return retryErr
		}
		checklist, confirmErr := service.Confirm(ctx, final.TaskID)
		if confirmErr != nil {
			return confirmErr
		}
		return encoder.Encode(demoOutput{
			Preview:      preview,
			InitialTask:  initial,
			FinalTask:    final,
			Progress:     progress,
			Confirmation: checklist,
		})
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
}

func loadFixture(path string) (migration.SourceDataset, error) {
	if path == "" {
		return fixture.LoadDefault()
	}
	return fixture.Load(path)
}
