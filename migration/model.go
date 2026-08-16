package migration

type SourceDataset struct {
	Students []SourceStudent `yaml:"students"`
	Classes  []SourceClass   `yaml:"classes"`
	Hours    []SourceHours   `yaml:"hours"`
	Exams    []SourceExam    `yaml:"exams"`
}

type SourceStudent struct {
	LegacyID       string `yaml:"legacy_id"`
	Name           string `yaml:"name"`
	IdentityNumber string `yaml:"identity_number"`
	ClassID        string `yaml:"class_id"`
}

type SourceClass struct {
	ClassID         string `yaml:"class_id"`
	Title           string `yaml:"title"`
	VehicleCategory string `yaml:"vehicle_category"`
}

type SourceHours struct {
	StudentID       string `yaml:"student_id"`
	TheoryMinutes   int    `yaml:"theory_minutes"`
	PracticeMinutes int    `yaml:"practice_minutes"`
}

type SourceExam struct {
	StudentID string `yaml:"student_id"`
	Subject   string `yaml:"subject"`
	Attempt   int    `yaml:"attempt"`
	Score     int    `yaml:"score"`
	Passed    bool   `yaml:"passed"`
}

type TargetLearner struct {
	RecordKey            string
	OriginKey            string
	DisplayName          string
	GovernmentCredential string
	Course               TargetCourse
	Attendance           TargetAttendance
	Assessments          []TargetAssessment
}

type TargetCourse struct {
	OfferingCode string
	Label        string
	PermitKind   string
}

type TargetAttendance struct {
	ClassroomMinutes int
	RoadMinutes      int
}

type TargetAssessment struct {
	StageCode string
	TryNumber int
	Mark      int
	Outcome   string
}

type PreviewRecord struct {
	DestinationKey  string `json:"destination_key"`
	DisplayName     string `json:"display_name"`
	Identity        string `json:"identity"`
	Program         string `json:"program"`
	PermitKind      string `json:"permit_kind"`
	TheoryMinutes   int    `json:"theory_minutes"`
	PracticeMinutes int    `json:"practice_minutes"`
	ExamCount       int    `json:"exam_count"`
}

type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusFailed    TaskStatus = "failed"
	StatusCanceled  TaskStatus = "canceled"
	StatusCompleted TaskStatus = "completed"
)

type RecordFailure struct {
	StudentKey string `json:"student_key"`
	Reason     string `json:"reason"`
	Attempts   int    `json:"attempts"`
}

type MigrationTask struct {
	TaskID    string          `json:"task_id"`
	Status    TaskStatus      `json:"status"`
	Attempt   int             `json:"attempt"`
	Total     int             `json:"total"`
	Processed int             `json:"processed"`
	Succeeded int             `json:"succeeded"`
	Failed    int             `json:"failed"`
	Failures  []RecordFailure `json:"failures,omitempty"`
}

type ProgressEvent struct {
	TaskID         string     `json:"task_id"`
	Status         TaskStatus `json:"status"`
	Attempt        int        `json:"attempt"`
	Processed      int        `json:"processed"`
	Total          int        `json:"total"`
	CurrentStudent string     `json:"current_student,omitempty"`
}

type ConfirmationItem struct {
	Code     string `json:"code"`
	Expected int    `json:"expected"`
	Actual   int    `json:"actual"`
	Passed   bool   `json:"passed"`
}

type ConfirmationChecklist struct {
	TaskID    string             `json:"task_id"`
	AllPassed bool               `json:"all_passed"`
	Items     []ConfirmationItem `json:"items"`
}

type RunOptions struct {
	OnProgress  func(ProgressEvent)
	AfterRecord func(ProgressEvent)
}
