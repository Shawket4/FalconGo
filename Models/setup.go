package Models

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Connect() {
	var err error

	if err := godotenv.Load(".env"); err != nil {
		log.Printf("ERROR: Failed to load .env file: %v", err)
	}

	// Get PostgreSQL connection details from environment
	dbHost := getEnv("DB_HOST", "")
	dbPort := getEnv("DB_PORT", "")
	dbUser := getEnv("DB_USER", "")
	dbPassword := getEnv("DB_PASSWORD", "")
	dbName := getEnv("DB_NAME", "")
	dbSSLMode := getEnv("DB_SSLMODE", "")

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=Africa/Cairo",
		dbHost, dbUser, dbPassword, dbName, dbPort, dbSSLMode,
	)

	// 🔥 FIX: Configure GORM properly for PostgreSQL sequences
	DB, err = gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true, // disables implicit prepared statement usage
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // Enable to debug: logger.Info
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
		PrepareStmt:              false, // 🔥 CRITICAL: Disable to allow sequences to work properly
		DisableNestedTransaction: false,
		AllowGlobalUpdate:        false,
		QueryFields:              true,
	})

	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	// Configure connection pool
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("Failed to get database instance: %v", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	log.Println("✅ PostgreSQL connected successfully")

	// 🔥 CRITICAL: Fix all sequences BEFORE running migrations
	fixAllSequences()

	// Run auto migrations
	runAutoMigrations()
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// 🔥 NEW: Fix all table sequences before migrations
func fixAllSequences() {
	log.Println("🔧 Fixing database sequences...")

	// List of all tables that use gorm.Model (have auto-increment ID)
	tables := []string{
		"users",
		"locations",
		"terminals",
		"land_marks",
		"fcm_tokens",
		"petro_app_stations",
		"speed_alerts",
		"daily_tasks",
		"drivers",
		"tablets",
		"cars",
		"expenses",
		"loans",
		"salaries",
		"tires",
		"trucks",
		"tire_positions",
		"driver_sessions",
		"location_pings",
		"fee_mappings",
		"parent_trips",
		"trips",
		"receipt_batches",
		"receipts",
		"receipt_steps",
		"etit_trip_routes",
		"fuel_events",
		"services",
		"oil_changes",
		"service_invoices",
		"inspection_items",
		"trip_summaries",
		"trip_legs",
		"vendors",
		"vendor_transactions",
	}

	for _, table := range tables {
		fixTableSequence(table)
	}

	log.Println("✅ All sequences fixed")
}

func fixTableSequence(tableName string) {
	sequenceName := tableName + "_id_seq"

	sqls := []string{
		// Create sequence if doesn't exist
		fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS %s`, sequenceName),

		// Set current value to max ID + 1
		fmt.Sprintf(`SELECT setval('%s', COALESCE((SELECT MAX(id) FROM %s), 0) + 1, false)`, sequenceName, tableName),

		// Set as default (ignore errors if column doesn't exist yet)
		fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN id SET DEFAULT nextval('%s'::regclass)`, tableName, sequenceName),

		// Own the sequence
		fmt.Sprintf(`ALTER SEQUENCE %s OWNED BY %s.id`, sequenceName, tableName),
	}

	for _, sql := range sqls {
		if err := DB.Exec(sql).Error; err != nil {
			// Silently ignore errors for tables that don't exist yet
			// They'll be created properly by AutoMigrate
		}
	}
}

func runAutoMigrations() {
	log.Println("Running database migrations...")

	// Phase 1: Independent tables (no foreign keys)
	DB.AutoMigrate(
		&User{},
		&Location{},
		&Terminal{},
		&LandMark{},
		&FCMToken{},
		&PetroAppStation{},
		&SpeedAlert{},
		&SpeedViolation{},
		&DailyTask{},
	)

	// Phase 2: Drivers first (many tables depend on it)
	DB.AutoMigrate(&Driver{})

	// Phase 3: Tablets and Cars (circular dependency handled by DisableForeignKeyConstraint)
	DB.AutoMigrate(&Tablet{})
	DB.AutoMigrate(&Car{})

	// Phase 4: Driver-related tables
	DB.AutoMigrate(
		&Expense{},
		&Loan{},
		&Salary{},
	)

	// Phase 5: Tire system
	DB.AutoMigrate(
		&Tire{},
		&Truck{},
		&TirePosition{},
	)

	// Phase 6: Session system (CRITICAL: Session before Pings)
	DB.AutoMigrate(&DriverSession{})
	DB.AutoMigrate(&LocationPing{})

	// Phase 7: Trip foundation
	DB.AutoMigrate(&FeeMapping{})
	DB.AutoMigrate(&ParentTrip{})

	// Phase 8: Trip details
	DB.AutoMigrate(
		&TripStruct{},
		&ReceiptBatch{},
		&Receipt{},
		&ReceiptStep{},
		&ETITTripRoute{},
	)

	// Phase 9: Vehicle maintenance
	DB.AutoMigrate(
		&FuelEvent{},
		&Service{},
		&OilChange{},
		&ServiceInvoice{},
		&InspectionItem{},
	)

	// Phase 10: Analytics
	DB.AutoMigrate(
		&TripSummary{},
		&TripLeg{},
	)

	// Phase 11: Vendor system
	DB.AutoMigrate(
		&Vendor{},
		&VendorTransaction{},
	)

	// Phase 12: Canonical locations (set-once terminal/drop-off pins)
	DB.AutoMigrate(
		&TerminalAlias{},
		&DropOffPoint{},
	)

	// 🔥 CRITICAL: Fix sequences again AFTER migrations
	// This ensures newly created tables have proper sequences
	fixAllSequences()

	// Create custom indexes
	createCustomIndexes()

	backfillDropOffPoints()

	log.Println("✅ Migrations completed")
}

// backfillDropOffPoints seeds drop_off_points from the legacy per-mapping
// coordinates in fee_mappings. Every distinct drop-off name gets a row; the
// pin is taken from the most recently updated mapping that has one. Names
// without any pinned mapping get a NULL pin and surface in the dashboard's
// "needs attention" inbox. Idempotent: existing rows are never overwritten.
func backfillDropOffPoints() {
	err := DB.Exec(`
        INSERT INTO drop_off_points (created_at, updated_at, name, latitude, longitude, pin_source)
        SELECT NOW(), NOW(), src.drop_off_point, src.lat, src.lng,
               CASE WHEN src.lat IS NOT NULL THEN 'backfill' ELSE '' END
          FROM (
            SELECT DISTINCT ON (drop_off_point)
                   drop_off_point,
                   NULLIF(latitude, 0)  AS lat,
                   NULLIF(longitude, 0) AS lng
              FROM fee_mappings
             WHERE deleted_at IS NULL AND drop_off_point <> ''
             ORDER BY drop_off_point,
                      (NULLIF(latitude, 0) IS NULL),
                      updated_at DESC
          ) src
        ON CONFLICT (name) DO NOTHING
    `).Error
	if err != nil {
		log.Printf("⚠️ drop_off_points backfill failed: %v", err)
	}
}

func createCustomIndexes() {
	log.Println("Creating custom indexes...")

	// Location Pings - CRITICAL for performance
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_location_pings_session_time 
        ON location_pings(session_id, time_stamp DESC) 
        WHERE session_id IS NOT NULL
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_location_pings_time 
        ON location_pings(time_stamp DESC)
    `)

	// Driver Sessions - Active sessions
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_driver_sessions_active 
        ON driver_sessions(driver_id, start_time DESC) 
        WHERE end_time IS NULL
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_driver_sessions_car 
        ON driver_sessions(car_id, start_time DESC)
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_driver_sessions_tablet 
        ON driver_sessions(tablet_id, start_time DESC)
    `)

	// Parent Trips
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_parent_trips_session 
        ON parent_trips(session_id) 
        WHERE session_id IS NOT NULL
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_parent_trips_date 
        ON parent_trips(date, created_at DESC)
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_parent_trips_driver 
        ON parent_trips(driver_id, created_at DESC)
    `)

	// Trip Summaries
	DB.Exec(`
        CREATE UNIQUE INDEX IF NOT EXISTS idx_trip_summaries_parent 
        ON trip_summaries(parent_trip_id) 
        WHERE deleted_at IS NULL
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_trip_summaries_time 
        ON trip_summaries(start_time DESC)
    `)

	// Trip Legs
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_trip_legs_summary_type 
        ON trip_legs(trip_summary_id, leg_type, sequence_number)
    `)

	// Container Trips
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_trips_parent 
        ON trips(parent_trip_id, created_at DESC) 
        WHERE parent_trip_id IS NOT NULL
    `)

	// Fee Mappings - Compound unique index
	DB.Exec(`
        CREATE UNIQUE INDEX IF NOT EXISTS idx_fee_mappings_lookup 
        ON fee_mappings(company, terminal, drop_off_point) 
        WHERE deleted_at IS NULL
    `)

	// Speed Alerts
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_speed_alerts_time 
        ON speed_alerts(parsed_time DESC) 
        WHERE parsed_time IS NOT NULL
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_speed_alerts_vehicle 
        ON speed_alerts(vehicle_id, parsed_time DESC)
    `)

	// Receipt Batches
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_receipt_batches_driver 
        ON receipt_batches(driver_id, scanned_at DESC)
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_receipt_batches_status 
        ON receipt_batches(status, scanned_at DESC)
    `)

	// Cars
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_cars_driver 
        ON cars(driver_id) 
        WHERE driver_id IS NOT NULL
    `)

	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_cars_tablet 
        ON cars(tablet_id) 
        WHERE tablet_id IS NOT NULL
    `)

	// Tablets
	DB.Exec(`
        CREATE INDEX IF NOT EXISTS idx_tablets_car 
        ON tablets(current_car_id) 
        WHERE current_car_id IS NOT NULL
    `)

	log.Println("✅ Custom indexes created")
}

func CreateDefaultPositions(db *gorm.DB, truckID uint) error {
	positions := []TirePosition{
		// Steering positions (2)
		{TruckID: truckID, PositionType: "steering", PositionIndex: 1, Side: "left"},
		{TruckID: truckID, PositionType: "steering", PositionIndex: 2, Side: "right"},

		// Head axle 1 positions (4) - Properly ordered
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "head_axle_1", PositionIndex: 4, Side: "right"},       // Outer Right

		// Head axle 2 positions (4) - Properly ordered
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "head_axle_2", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 1 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_1", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 2 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_2", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 3 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_3", PositionIndex: 4, Side: "right"},       // Outer Right

		// Trailer axle 4 positions - Properly ordered
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 1, Side: "left"},        // Outer Left
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 2, Side: "inner_left"},  // Inner Left
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 3, Side: "inner_right"}, // Inner Right
		{TruckID: truckID, PositionType: "trailer_axle_4", PositionIndex: 4, Side: "right"},       // Outer Right

		// Spare positions (2)
		{TruckID: truckID, PositionType: "spare", PositionIndex: 1, Side: "none"},
		{TruckID: truckID, PositionType: "spare", PositionIndex: 2, Side: "none"},
	}

	result := db.Create(&positions)
	return result.Error
}
