package migration

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type SourceRepository interface {
	ListStudents(context.Context) ([]SourceStudent, error)
	Student(context.Context, string) (SourceStudent, error)
	Class(context.Context, string) (SourceClass, error)
	Hours(context.Context, string) (SourceHours, error)
	Exams(context.Context, string) ([]SourceExam, error)
}

type MemorySource struct {
	mu       sync.RWMutex
	students map[string]SourceStudent
	classes  map[string]SourceClass
	hours    map[string]SourceHours
	exams    map[string][]SourceExam
}

func NewMemorySource(dataset SourceDataset) (*MemorySource, error) {
	repository := &MemorySource{
		students: make(map[string]SourceStudent, len(dataset.Students)),
		classes:  make(map[string]SourceClass, len(dataset.Classes)),
		hours:    make(map[string]SourceHours, len(dataset.Hours)),
		exams:    make(map[string][]SourceExam),
	}
	for _, class := range dataset.Classes {
		if class.ClassID == "" {
			return nil, fmt.Errorf("source class has an empty key")
		}
		if _, exists := repository.classes[class.ClassID]; exists {
			return nil, fmt.Errorf("duplicate source class %s", class.ClassID)
		}
		repository.classes[class.ClassID] = class
	}
	for _, student := range dataset.Students {
		if student.LegacyID == "" {
			return nil, fmt.Errorf("source student has an empty key")
		}
		if _, exists := repository.students[student.LegacyID]; exists {
			return nil, fmt.Errorf("duplicate source student %s", student.LegacyID)
		}
		repository.students[student.LegacyID] = student
	}
	for _, hours := range dataset.Hours {
		if _, exists := repository.hours[hours.StudentID]; exists {
			return nil, fmt.Errorf("duplicate source hours for %s", hours.StudentID)
		}
		repository.hours[hours.StudentID] = hours
	}
	for _, exam := range dataset.Exams {
		repository.exams[exam.StudentID] = append(repository.exams[exam.StudentID], exam)
	}
	for studentID := range repository.exams {
		sort.Slice(repository.exams[studentID], func(i, j int) bool {
			left := repository.exams[studentID][i]
			right := repository.exams[studentID][j]
			if left.Subject != right.Subject {
				return left.Subject < right.Subject
			}
			return left.Attempt < right.Attempt
		})
	}
	return repository, nil
}

func (r *MemorySource) ListStudents(_ context.Context) ([]SourceStudent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	students := make([]SourceStudent, 0, len(r.students))
	for _, student := range r.students {
		students = append(students, student)
	}
	sort.Slice(students, func(i, j int) bool {
		return students[i].LegacyID < students[j].LegacyID
	})
	return students, nil
}

func (r *MemorySource) Student(_ context.Context, studentID string) (SourceStudent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	student, ok := r.students[studentID]
	if !ok {
		return SourceStudent{}, fmt.Errorf("source student %s not found", studentID)
	}
	return student, nil
}

func (r *MemorySource) Class(_ context.Context, classID string) (SourceClass, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	class, ok := r.classes[classID]
	if !ok {
		return SourceClass{}, fmt.Errorf("source class %s not found", classID)
	}
	return class, nil
}

func (r *MemorySource) Hours(_ context.Context, studentID string) (SourceHours, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hours, ok := r.hours[studentID]
	if !ok {
		return SourceHours{}, fmt.Errorf("source hours for %s not found", studentID)
	}
	return hours, nil
}

func (r *MemorySource) Exams(_ context.Context, studentID string) ([]SourceExam, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exams := r.exams[studentID]
	result := make([]SourceExam, len(exams))
	copy(result, exams)
	return result, nil
}
