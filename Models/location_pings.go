package Models

import (
	"time"

	"gorm.io/gorm"
)

// location_ping.go - Already good, but add validation
type LocationPing struct {
	gorm.Model

	SessionID *uint          `json:"session_id" gorm:"index:idx_session_time;constraint:OnUpdate:CASCADE,OnDelete:SET NULL"`
	Session   *DriverSession `json:"session,omitempty" gorm:"foreignKey:SessionID"`

	Lat       float64   `json:"lat" gorm:"not null;check:lat >= -90 AND lat <= 90"`
	Lng       float64   `json:"lng" gorm:"not null;check:lng >= -180 AND lng <= 180"`
	TimeStamp time.Time `json:"time_stamp" gorm:"index:idx_session_time;not null"`

	Speed    *float64 `json:"speed" gorm:"check:speed >= 0"`
	Accuracy *float64 `json:"accuracy" gorm:"check:accuracy >= 0"`
	Altitude *float64 `json:"altitude"`
	Bearing  *float64 `json:"bearing" gorm:"check:bearing >= 0 AND bearing < 360"`
}

// TableName specifies the table name for LocationPing model
func (LocationPing) TableName() string {
	return "location_pings"
}

// Helper method to get driver ID from session
func (lp *LocationPing) GetDriverID(db *gorm.DB) uint {
	if lp.Session.ID != 0 {
		return lp.Session.DriverID
	}
	// Load session if not preloaded
	var session DriverSession
	db.First(&session, lp.SessionID)
	return session.DriverID
}
