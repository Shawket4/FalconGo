package Models

import (
	"time"

	"gorm.io/gorm"
)

type DailyTask struct {
	gorm.Model
	TaskName           string     `json:"task_name"`
	TaskType           string     `json:"task_type"`
	AssignedDate       time.Time  `json:"assigned_date"`
	CompletedAt        *time.Time `json:"completed_at"`
	IsCompleted        bool       `json:"is_completed"`
	ValidationData     string     `json:"validation_data"`
	CompletedBy        string     `json:"completed_by"`
	ValidationError    string     `json:"validation_error,omitempty"`
	RequiresValidation bool       `json:"requires_validation"`
}
