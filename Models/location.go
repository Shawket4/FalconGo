package Models

import "gorm.io/gorm"

type Location struct {
	gorm.Model
	Name    string `json:"name"`
	Address string `json:"address"`
}

type Terminal struct {
	gorm.Model
	Name      string   `json:"name" gorm:"unique"`
	Address   string   `json:"address"`
	Latitude  float64  `json:"lat"`
	Longitude float64  `json:"long"`
	// Geofence radius override in meters. NULL means the consumer's default
	// (trip auditor defaults to 500m for terminals).
	RadiusM *float64 `json:"radius_m"`
}

// TerminalAlias maps the free-text terminal strings found in trips and
// fee_mappings (e.g. "Suez", "طاقة السويس") to a canonical Terminal row.
type TerminalAlias struct {
	gorm.Model
	Alias      string `json:"alias" gorm:"uniqueIndex"`
	TerminalID uint   `json:"terminal_id" gorm:"index"`

	Terminal Terminal `json:"terminal" gorm:"foreignKey:TerminalID"`
}

// DropOffPoint is the single source of truth for drop-off coordinates.
// Fee mappings and the trip auditor reference it by Name; a pin is set once
// here instead of per fee mapping. Latitude/Longitude are NULL until someone
// pins the location on the dashboard.
type DropOffPoint struct {
	gorm.Model
	Name      string   `json:"name" gorm:"uniqueIndex"`
	Latitude  *float64 `json:"lat"`
	Longitude *float64 `json:"long"`
	// Geofence radius override in meters. NULL means the consumer's default
	// (trip auditor defaults to 300m for drop-offs).
	RadiusM *float64 `json:"radius_m"`
	// Where the current pin came from: "backfill", "manual", "gps_suggested".
	PinSource string `json:"pin_source"`
}

func (DropOffPoint) TableName() string {
	return "drop_off_points"
}
