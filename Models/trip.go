package Models

import (
	"time"

	"gorm.io/datatypes"
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
	// FK to the canonical terminals row. The legacy Terminal string stays in
	// sync (set to the terminal's canonical name) during the transition.
	TerminalID *uint  `json:"terminal_id" gorm:"index"`
	Date       string `json:"date"`
	Author     string `json:"author"`
	Overwriter string `json:"overwriter"`
	// Relationships
	ContainerTrips []TripStruct `json:"container_trips" gorm:"foreignKey:ParentTripID"`

	// Calculated/Aggregated fields
	TotalRevenue    float64        `json:"total_revenue" gorm:"-"`
	TotalContainers int            `json:"total_containers" gorm:"-"`
	ReceiptBatch    *ReceiptBatch  `gorm:"foreignKey:AssignedToTripID" json:"receipt_batch,omitempty"`
	SessionID       *uint          `json:"session_id" gorm:"index"`
	Session         *DriverSession `json:"session,omitempty" gorm:"foreignKey:SessionID"`
}

// TripCompartment is one compartment of the car assigned to a drop. Index is
// positional into the car's json_compartments; Volume and GasType are stored
// alongside it so a trip row stays readable even after the car's compartment
// layout is edited.
type TripCompartment struct {
	Index   int    `json:"index"`
	Volume  int    `json:"volume"`
	GasType string `json:"gas_type"`
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
	Company  string `json:"company"`
	Terminal string `json:"terminal"` // Added Terminal field (was PickUpPoint)
	// FK to the canonical terminals row. The legacy Terminal string stays in
	// sync (set to the terminal's canonical name) during the transition.
	TerminalID   *uint  `json:"terminal_id" gorm:"index"`
	DropOffPoint string `json:"drop_off_point"` // Added DropOffPoint field

	// Location details
	LocationName string `json:"location_name"`
	Capacity     int    `json:"capacity"`
	// GasType is derived: the distinct set of the compartments' products,
	// comma-joined in canonical order (e.g. "80,92"). Kept as a plain string
	// so every existing reader and report is unaffected.
	GasType string `json:"gas_type"`
	// Compartments records which of the car's compartments this drop was
	// loaded from, as a JSON array of TripCompartment. NULL on trips created
	// before compartment planning existed — readers must tolerate that.
	Compartments datatypes.JSON `json:"compartments"`

	// Trip details
	Date string `json:"date"`
	// Revenue mirrors BaseRevenue (0 while unpriced) so readers that predate
	// stored pricing see the real figure. It is owned by the pricing engine:
	// no write path sets it from a request any more.
	Revenue   float64 `json:"revenue"`
	Mileage   float64 `json:"mileage"`
	ReceiptNo string  `json:"receipt_no"`

	// Stored pricing, written only by the pricing package (see
	// pricing/engine.go). BaseRevenue is this row's own revenue, excluding
	// car rental, under the rule effective on Date; RevenueVAT is VAT on it.
	// Car rental is per truck-month, in truck_month_charges. PricedKm is the
	// per-km basis for per-km companies: the logical trip's furthest drop on
	// the one row that holds it, 0 on its siblings, NULL for volume-priced
	// rows. A row that could not be priced has PricingError set and NULL
	// revenue — never a silent 0.
	BaseRevenue   *float64   `json:"base_revenue" gorm:"type:numeric"`
	RevenueVAT    *float64   `json:"revenue_vat" gorm:"column:revenue_vat;type:numeric"`
	PricedKm      *float64   `json:"priced_km" gorm:"type:numeric"`
	PricingRuleID *uint      `json:"pricing_rule_id"`
	PricedAt      *time.Time `json:"priced_at" gorm:"type:timestamptz"`
	PricingError  *string    `json:"pricing_error" gorm:"type:text"`
	// Set when the receipt number knowingly falls outside the terminal's
	// serialization pattern (e.g. a manual replacement book).
	ReceiptOverride bool `json:"receipt_override" gorm:"not null;default:false"`

	// Calculated fields
	Distance float64 `json:"distance" gorm:"-"` // Distance from fee mapping, not stored
	Fee      float64 `json:"fee" gorm:"-"`      // Fee from fee mapping, not stored
	// Receipt number vs. the terminal's serial pattern for this company:
	// nil = no active pattern to check against, true/false otherwise.
	ReceiptValid *bool         `json:"receipt_valid" gorm:"-"`
	Location     LandMark      `json:"location" gorm:"-"`
	ParentTrip   *ParentTrip   `json:"parent_trip,omitempty" gorm:"foreignKey:ParentTripID"` // Add this line
	ReceiptSteps []ReceiptStep `json:"receipt_steps" gorm:"foreignKey:TripID;constraint:OnDelete:CASCADE"`
}

// TripSummary stores comprehensive analysis of a completed parent trip
// Models/TripSummary.go
type TripSummary struct {
	gorm.Model
	ParentTripID uint `gorm:"uniqueIndex;not null"`

	// Trip Boundaries
	StartLocationLat float64
	StartLocationLng float64
	EndLocationLat   float64
	EndLocationLng   float64
	StartTime        time.Time
	EndTime          time.Time
	TripDuration     int // seconds

	// Distance Metrics
	ActualDistance    float64 // km
	OSRMDistance      float64 // km
	DistanceDeviation float64 // km
	RouteEfficiency   float64 // percentage

	// Time Metrics
	OSRMDuration  int     // seconds
	TimeDeviation int     // seconds
	AverageSpeed  float64 // km/h

	// Delivery Performance
	TotalDeliveries   int
	SkippedDeliveries string `gorm:"type:TEXT" json:"skipped_deliveries,omitempty"`

	// Status
	IsComplete     bool
	ReturnedToBase bool
	ReturnTime     *time.Time

	// ADDED: OSRM Geometry
	OSRMGeometry string `gorm:"type:TEXT" json:"osrm_geometry,omitempty"`

	// Relations (stored in DB)
	ParentTrip  ParentTrip `gorm:"foreignKey:ParentTripID"`
	ActualLegs  []TripLeg  `gorm:"foreignKey:TripSummaryID;constraint:OnDelete:CASCADE"`
	OptimalLegs []TripLeg  `gorm:"foreignKey:TripSummaryID;constraint:OnDelete:CASCADE"`

	// Location Pings (NOT stored, populated on fetch)
	LocationPings []LocationPing `gorm:"-" json:"location_pings,omitempty"`
}

type TripLeg struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	TripSummaryID   uint      `json:"trip_summary_id" gorm:"index"`
	LegType         string    `json:"leg_type" gorm:"type:varchar(20)"`
	SequenceNumber  int       `json:"sequence_number"`
	TripStructID    *uint     `json:"trip_struct_id,omitempty"`
	ContainerIDs    string    `json:"container_ids,omitempty" gorm:"type:TEXT"`
	FromLocationLat float64   `json:"from_location_lat"`
	FromLocationLng float64   `json:"from_location_lng"`
	ToLocationLat   float64   `json:"to_location_lat"`
	ToLocationLng   float64   `json:"to_location_lng"`
	FromName        string    `json:"from_name" gorm:"type:varchar(255)"`
	ToName          string    `json:"to_name" gorm:"type:varchar(255)"`
	DepartureTime   time.Time `json:"departure_time"`
	ArrivalTime     time.Time `json:"arrival_time"`
	DwellTime       int       `json:"dwell_time"`
	TravelDuration  int       `json:"travel_duration"`
	ActualDistance  float64   `json:"actual_distance"`
	OSRMDistance    float64   `json:"osrm_distance"`
	AverageSpeed    float64   `json:"average_speed"`
	MaxSpeed        float64   `json:"max_speed"`
	IsOptimal       bool      `json:"is_optimal"`

	// ADDED: OSRM Geometry for individual leg
	OSRMGeometry string `gorm:"type:TEXT" json:"osrm_geometry,omitempty"`

	LocationPings []LocationPing `json:"location_pings,omitempty" gorm:"-"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
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
	Company  string `json:"company"`  // Company associated with this mapping
	Terminal string `json:"terminal"` // Pickup terminal
	// FK to the canonical terminals row (kept in sync with the Terminal string).
	TerminalID   *uint   `json:"terminal_id" gorm:"index"`
	DropOffPoint string  `json:"drop_off_point"` // Drop-off location
	Distance     float64 `json:"distance"`       // Distance in kilometers
	Fee          float64 `json:"fee"`            // Associated fee for this mapping
	// Coordinates are NOT stored on this table. drop_off_points is the
	// canonical home for a pin, and a copy here went stale the moment someone
	// corrected one — enrichment then routed to yesterday's location while the
	// locations screen showed today's.
	//
	// They stay on the struct, and stay in the JSON, because the fee-mappings
	// screen uses them for the "located" badge, the stats, the export column
	// and the pin dialog. They are populated by HydrateFeeMappingCoords, not
	// by the database.
	Latitude     float64 `json:"lat" gorm:"-"`
	Longitude    float64 `json:"long" gorm:"-"`
	OSRMDistance float64 `json:"osrm_distance"`
	OSRMDuration float64 `json:"osrm_duration"` // Duration in minutes
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
