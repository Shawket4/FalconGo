package Models

import (
	"time"

	"gorm.io/gorm"
)

type DriverSession struct {
	gorm.Model
	DriverID uint   `json:"driver_id" gorm:"index:idx_driver_time;not null"`
	Driver   Driver `json:"driver" gorm:"foreignKey:DriverID"`

	CarID uint `json:"car_id" gorm:"index;not null"`
	Car   Car  `json:"car" gorm:"foreignKey:CarID"`

	TabletID uint   `json:"tablet_id" gorm:"index;not null"`
	Tablet   Tablet `json:"tablet" gorm:"foreignKey:TabletID"`

	StartTime time.Time  `json:"start_time" gorm:"not null;index:idx_driver_time"`
	EndTime   *time.Time `json:"end_time" gorm:"index:idx_active_sessions"` // NULL = active session

	StartOdometer *float64 `json:"start_odometer"`
	EndOdometer   *float64 `json:"end_odometer"`

	// Calculated fields
	DurationMinutes *int     `json:"duration_minutes" gorm:"-"` // Calculated, not stored
	TotalDistance   *float64 `json:"total_distance" gorm:"-"`   // Calculated from location pings

	// Relationships
	LocationPings []LocationPing `json:"location_pings,omitempty" gorm:"foreignKey:SessionID"`
}

// TableName specifies the table name for DriverSession model
func (DriverSession) TableName() string {
	return "driver_sessions"
}

// IsActive checks if the session is currently active
func (s *DriverSession) IsActive() bool {
	return s.EndTime == nil
}

// CalculateDuration calculates session duration in minutes
func (s *DriverSession) CalculateDuration() int {
	if s.EndTime == nil {
		// Active session - calculate from start to now
		return int(time.Since(s.StartTime).Minutes())
	}
	// Completed session
	return int(s.EndTime.Sub(s.StartTime).Minutes())
}
