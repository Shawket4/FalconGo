package Models

import (
	"crypto/sha256"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type SpeedAlert struct {
	gorm.Model
	VehicleID    string
	PlateNo      string
	Speed        int
	Timestamp    string
	ParsedTime   time.Time
	Latitude     string
	Longitude    string
	ExceedsBy    int
	AlertedAdmin bool   `json:"alerted_admin"`
	Hash         string `gorm:"uniqueIndex;size:64"`
}

// BeforeCreate automatically generates hash before saving
func (s *SpeedAlert) BeforeCreate(tx *gorm.DB) error {
	if s.Hash == "" {
		data := fmt.Sprintf("%s|%s|%s|%s", s.VehicleID, s.Latitude, s.Longitude, s.Timestamp)
		hash := sha256.Sum256([]byte(data))
		s.Hash = fmt.Sprintf("%x", hash)
	}
	return nil
}
