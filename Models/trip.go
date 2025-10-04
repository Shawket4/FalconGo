package Models

import (
	"time"

	"gorm.io/gorm"
)

type ParentTrip struct {
	gorm.Model
	CarID        uint   `json:"car_id"`
	DriverID     uint   `json:"driver_id"`
	CarNoPlate   string `json:"car_no_plate"`
	DriverName   string `json:"driver_name"`
	Transporter  string `json:"transporter"`
	TankCapacity int    `json:"tank_capacity"`

	Company  string `json:"company"`
	Terminal string `json:"terminal"`
	Date     string `json:"date"`

	// Relationships
	ContainerTrips []TripStruct `json:"container_trips" gorm:"foreignKey:ParentTripID"`

	// Calculated/Aggregated fields
	TotalRevenue    float64 `json:"total_revenue" gorm:"-"`
	TotalContainers int     `json:"total_containers" gorm:"-"`
}

// TripStruct represents a trip record with additional fields for terminal and drop-off points
type TripStruct struct {
	gorm.Model
	ParentTripID *uint  `json:"parent_trip_id" gorm:"index"` // Nullable for existing data
	CarID        uint   `json:"car_id"`
	DriverID     uint   `json:"driver_id"`
	CarNoPlate   string `json:"car_no_plate"`
	DriverName   string `json:"driver_name"`
	Transporter  string `json:"transporter"`
	TankCapacity int    `json:"tank_capacity"`

	// Company and related fields for dropdown selection
	Company      string `json:"company"`
	Terminal     string `json:"terminal"`       // Added Terminal field (was PickUpPoint)
	DropOffPoint string `json:"drop_off_point"` // Added DropOffPoint field

	// Location details
	LocationName string `json:"location_name"`
	Capacity     int    `json:"capacity"`
	GasType      string `json:"gas_type"`

	// Trip details
	Date      string  `json:"date"`
	Revenue   float64 `json:"revenue"`
	Mileage   float64 `json:"mileage"`
	ReceiptNo string  `json:"receipt_no"`

	// Calculated fields
	Distance     float64       `json:"distance" gorm:"-"` // Distance from fee mapping, not stored
	Fee          float64       `json:"fee" gorm:"-"`      // Fee from fee mapping, not stored
	ReceiptSteps []ReceiptStep `json:"receipt_steps" gorm:"foreignKey:TripID;constraint:OnDelete:CASCADE"`
}

type ReceiptStep struct {
	gorm.Model
	TripID     uint      `json:"trip_id" gorm:"index;not null"`
	Location   string    `json:"location" gorm:"type:varchar(20);not null"` // "Garage" or "Office"
	ReceivedBy string    `json:"received_by" gorm:"type:varchar(255);not null"`
	ReceivedAt time.Time `json:"received_at" gorm:"not null"`
	StepOrder  int       `json:"step_order" gorm:"not null"` // 1 for first step, 2 for second, etc.
	Stamped    bool      `json:"stamped" gorm:"default:false"`
	Notes      string    `json:"notes" gorm:"type:text"`
}

// TableName specifies the table name for the Trip model
func (TripStruct) TableName() string {
	return "trips"
}

type ETITTripRoute struct {
	gorm.Model
	TripID               uint   `json:"trip_id"`
	CarID                uint   `json:"car_id"`
	EtitCarID            string `json:"etit_car_id"`
	FromTime             string `json:"from_time"`
	ToTime               string `json:"to_time"`
	Coordinates          string `json:"coordinates"`  // JSON string of coordinates
	Stops                string `json:"stops"`        // JSON string of stops
	TripSummary          string `json:"trip_summary"` // JSON string of trip summary
	TotalPoints          int    `json:"total_points"`
	TotalStops           int    `json:"total_stops"`
	TotalMileage         string `json:"total_mileage"`
	TotalActiveTime      string `json:"total_active_time"`
	TotalPassiveTime     string `json:"total_passive_time"`
	TotalIdleTime        string `json:"total_idle_time"`
	TotalFuelConsumption string `json:"total_fuel_consumption"`
	DriverName           string `json:"driver_name"`
	NumberofStops        string `json:"number_of_stops"`
}

// FeeMapping represents a mapping between terminals, drop-off points, distance, and fee
type FeeMapping struct {
	gorm.Model
	Company      string  `json:"company"`        // Company associated with this mapping
	Terminal     string  `json:"terminal"`       // Pickup terminal
	DropOffPoint string  `json:"drop_off_point"` // Drop-off location
	Distance     float64 `json:"distance"`       // Distance in kilometers
	Fee          float64 `json:"fee"`            // Associated fee for this mapping
	Latitude     float64 `json:"lat"`
	Longitude    float64 `json:"long"`
	OSRMDistance float64 `json:"osrm_distance"`
}

// Ensure uniqueness of company, terminal, and drop-off point combination
func (FeeMapping) TableName() string {
	return "fee_mappings"
}

// Setup indexes for FeeMapping
func SetupFeeMappingIndexes(db *gorm.DB) error {
	// Create unique index for company, terminal, and drop-off point
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_company_terminal_dropoff ON fee_mappings (company, terminal, drop_off_point) WHERE deleted_at IS NULL").Error
}
