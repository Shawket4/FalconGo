package Models

import (
	"time"

	"gorm.io/gorm"
)

// SpeedViolation represents a traffic speed violation record
type SpeedViolation struct {
	gorm.Model
	CarID             uint      `json:"car_id" gorm:"index"`                            // Foreign key to cars table
	Car               *Car      `gorm:"foreignKey:car_id" json:"car,omitempty"`         // Relationship to Car
	DriverID          *uint     `json:"driver_id" gorm:"index"`                         // Driver at time of violation
	Driver            *Driver   `gorm:"foreignKey:driver_id" json:"driver,omitempty"`   // Relationship to Driver
	ViolationLocation string    `gorm:"type:text" json:"violation_location"`            // Where the violation occurred
	ViolationTime     time.Time `gorm:"type:date;not null" json:"violation_date"`       // Date of violation
	FineAmount        float64   `gorm:"type:decimal(10,2);not null" json:"fine_amount"` // Individual fine amount
	RawMessage        string    `gorm:"type:text" json:"raw_message"`                   // Original Arabic message
	ProcessedAt       time.Time `gorm:"not null" json:"processed_at"`                   // When we processed the message

	// Driver Detection Fields
	DriverConfidence    float64 `gorm:"type:decimal(5,2)" json:"driver_confidence"`     // 0-100 confidence score
	NearestTripBeforeID *uint   `json:"nearest_trip_before_id,omitempty" gorm:"index"`  // Trip that ended before violation
	NearestTripAfterID  *uint   `json:"nearest_trip_after_id,omitempty" gorm:"index"`   // Trip that started after violation
	TripBeforeGap       *int    `json:"trip_before_gap_minutes,omitempty"`              // Minutes from trip end to violation
	TripAfterGap        *int    `json:"trip_after_gap_minutes,omitempty"`               // Minutes from violation to trip start
	MatchMethod         string  `gorm:"type:varchar(50)" json:"match_method,omitempty"` // How driver was determined
}

// TableName specifies the table name for GORM
func (SpeedViolation) TableName() string {
	return "speed_violations"
}

// BeforeCreate hook to set defaults
func (v *SpeedViolation) BeforeCreate(tx *gorm.DB) error {
	if v.ProcessedAt.IsZero() {
		v.ProcessedAt = time.Now()
	}
	return nil
}

// DriverMatchResult contains information about how a driver was matched
type DriverMatchResult struct {
	DriverID      uint
	DriverName    string
	Confidence    float64 // 0-100
	MatchMethod   string  // "during_trip", "between_trips", "before_first_trip", "after_last_trip", "single_driver", "no_trips_found"
	TripBeforeID  *uint
	TripAfterID   *uint
	TripBeforeGap int // minutes
	TripAfterGap  int // minutes
	Explanation   string
}
