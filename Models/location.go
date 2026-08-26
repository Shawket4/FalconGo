package Models

import (
	"time"

	"gorm.io/gorm"
)

type Location struct {
	gorm.Model
	Name    string `json:"name"`
	Address string `json:"address"`
}

type Terminal struct {
	gorm.Model
	Name      string  `json:"name" gorm:"unique"`
	Address   string  `json:"address"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"long"`
	// Geofence radius override in meters. NULL means the consumer's default
	// (trip auditor defaults to 500m for terminals).
	RadiusM *float64 `json:"radius_m"`
	// Where the current pin came from: "" (unpinned/legacy), "manual",
	// "gps_suggested" (auto-applied by the etit-proxy calibration job).
	PinSource string `json:"pin_source" gorm:"default:''"`
}

// TerminalCompany is the per-company allowlist: a terminal can only be picked
// for a trip when a (terminal, company) row exists here. Seeded from the
// historical trips/fee_mappings data and extended via POST /api/terminals.
type TerminalCompany struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	TerminalID uint      `json:"terminal_id" gorm:"not null;uniqueIndex:idx_terminal_companies_terminal_company"`
	Company    string    `json:"company" gorm:"not null;uniqueIndex:idx_terminal_companies_terminal_company"`
	CreatedAt  time.Time `json:"created_at"`

	Terminal Terminal `json:"-" gorm:"foreignKey:TerminalID"`
}

func (TerminalCompany) TableName() string {
	return "terminal_companies"
}

// TerminalReceiptPattern validates receipt numbers per (terminal, company)
// with an anchored regex. Company NULL means the pattern applies to every
// company loading at that terminal; a company-specific row wins over the
// NULL-company row. Uniqueness is enforced by a raw expression index on
// terminal_id plus company coalesced to the empty string -- see
// migrateTerminalNormalization -- because a plain UNIQUE(terminal_id, company)
// treats NULLs as distinct.
type TerminalReceiptPattern struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	TerminalID  uint      `json:"terminal_id" gorm:"not null;index"`
	Company     *string   `json:"company"`
	Pattern     string    `json:"pattern" gorm:"not null"`
	Description string    `json:"description"`
	Active      bool      `json:"active" gorm:"default:true"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Terminal Terminal `json:"-" gorm:"foreignKey:TerminalID"`
}

func (TerminalReceiptPattern) TableName() string {
	return "terminal_receipt_patterns"
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
