package Models

import (
	"Falcon/Observability"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
)

// RunSessionSystemMigration - Main migration function
func RunSessionSystemMigration(db *gorm.DB) error {
	log.Println("Starting session system migration...")

	// Step 1: Create new tables
	if err := createNewTables(db); err != nil {
		return err
	}

	// Step 2: Modify existing tables
	if err := modifyExistingTables(db); err != nil {
		return err
	}

	// Step 3: Handle LocationPing migration
	if err := migrateLocationPings(db); err != nil {
		return err
	}

	// Step 4: Cleanup old columns
	if err := cleanupOldColumns(db); err != nil {
		return err
	}

	log.Println("✅ Session system migration completed successfully!")
	return nil
}

func createNewTables(db *gorm.DB) error {
	log.Println("📦 Creating new tables...")

	// Create Tablets table
	if !db.Migrator().HasTable(&Tablet{}) {
		log.Println("  Creating 'tablets' table...")
		if err := db.AutoMigrate(&Tablet{}); err != nil {
			return fmt.Errorf("failed to create tablets table: %w", err)
		}
		log.Println("  ✅ 'tablets' table created")
	} else {
		log.Println("  ⏭️  'tablets' table already exists")
	}

	// Create DriverSessions table
	if !db.Migrator().HasTable(&DriverSession{}) {
		log.Println("  Creating 'driver_sessions' table...")
		if err := db.AutoMigrate(&DriverSession{}); err != nil {
			return fmt.Errorf("failed to create driver_sessions table: %w", err)
		}
		log.Println("  ✅ 'driver_sessions' table created")
	} else {
		log.Println("  ⏭️  'driver_sessions' table already exists")
	}

	return nil
}

func modifyExistingTables(db *gorm.DB) error {
	log.Println("🔧 Modifying existing tables...")

	// Detect database type for special handling
	dbType := db.Dialector.Name()
	log.Printf("  Database type: %s", dbType)

	// Add TabletID to cars table
	if !db.Migrator().HasColumn(&Car{}, "tablet_id") {
		log.Println("  Adding 'tablet_id' column to 'cars' table...")
		if err := db.Migrator().AddColumn(&Car{}, "tablet_id"); err != nil {
			return fmt.Errorf("failed to add tablet_id to cars: %w", err)
		}
		log.Println("  ✅ 'tablet_id' column added")
	} else {
		log.Println("  ⏭️  'tablet_id' column already exists in 'cars'")
	}

	// Add PIN to drivers table (with special handling for SQLite)
	if !db.Migrator().HasColumn(&Driver{}, "pin") {
		log.Println("  Adding 'pin' column to 'drivers' table...")

		if dbType == "sqlite" {
			// SQLite: Add column without NOT NULL constraint first
			if err := db.Exec("ALTER TABLE drivers ADD COLUMN pin TEXT").Error; err != nil {
				return fmt.Errorf("failed to add pin to drivers: %w", err)
			}
		} else {
			// MySQL/PostgreSQL: Use standard migration
			if err := db.Migrator().AddColumn(&Driver{}, "pin"); err != nil {
				return fmt.Errorf("failed to add pin to drivers: %w", err)
			}
		}

		log.Println("  ✅ 'pin' column added")
	} else {
		log.Println("  ⏭️  'pin' column already exists in 'drivers'")
	}

	// Add SessionID to parent_trips table
	if !db.Migrator().HasColumn(&ParentTrip{}, "session_id") {
		log.Println("  Adding 'session_id' column to 'parent_trips' table...")
		if err := db.Migrator().AddColumn(&ParentTrip{}, "session_id"); err != nil {
			return fmt.Errorf("failed to add session_id to parent_trips: %w", err)
		}
		log.Println("  ✅ 'session_id' column added")
	} else {
		log.Println("  ⏭️  'session_id' column already exists in 'parent_trips'")
	}

	return nil
}

func migrateLocationPings(db *gorm.DB) error {
	log.Println("📍 Migrating location_pings table...")

	dbType := db.Dialector.Name()

	// Check if migration is needed
	hasDriverID := db.Migrator().HasColumn(&LocationPing{}, "driver_id")
	hasSessionID := db.Migrator().HasColumn(&LocationPing{}, "session_id")

	if !hasDriverID && hasSessionID {
		log.Println("  ⏭️  Location pings already migrated")
		return nil
	}

	if hasDriverID {
		log.Println("  🔄 Found old 'driver_id' column - migrating...")

		// Create backup (syntax varies by database)
		log.Println("  📦 Creating backup table...")
		var backupQuery string

		switch dbType {
		case "sqlite":
			backupQuery = "CREATE TABLE IF NOT EXISTS location_pings_backup AS SELECT * FROM location_pings"
		case "mysql":
			backupQuery = "CREATE TABLE IF NOT EXISTS location_pings_backup AS SELECT * FROM location_pings"
		case "postgres":
			backupQuery = "CREATE TABLE IF NOT EXISTS location_pings_backup AS TABLE location_pings"
		default:
			backupQuery = "CREATE TABLE IF NOT EXISTS location_pings_backup AS SELECT * FROM location_pings"
		}

		if err := db.Exec(backupQuery).Error; err != nil {
			log.Printf("  ⚠️  Warning: Failed to create backup: %v", err)
		} else {
			log.Println("  ✅ Backup created: location_pings_backup")
		}

		// Add session_id column
		if !hasSessionID {
			log.Println("  Adding 'session_id' column...")

			if dbType == "sqlite" {
				// SQLite: Add nullable column
				if err := db.Exec("ALTER TABLE location_pings ADD COLUMN session_id INTEGER").Error; err != nil {
					return fmt.Errorf("failed to add session_id: %w", err)
				}
			} else {
				// MySQL/PostgreSQL
				if err := db.Migrator().AddColumn(&LocationPing{}, "session_id"); err != nil {
					return fmt.Errorf("failed to add session_id: %w", err)
				}
			}
			log.Println("  ✅ 'session_id' column added")
		}

		// Add optional GPS fields
		addOptionalColumns(db)

		// Note about data migration
		log.Println("  ⚠️  IMPORTANT: Old location pings have 'driver_id'")
		log.Println("  ⚠️  New pings will use 'session_id'")
		log.Println("  ⚠️  Run migrations.CreateLegacySessions() to migrate historical data")
	} else {
		// Fresh installation - just create with new structure
		log.Println("  Creating fresh 'location_pings' table...")
		if err := db.AutoMigrate(&LocationPing{}); err != nil {
			return fmt.Errorf("failed to create location_pings: %w", err)
		}
		log.Println("  ✅ 'location_pings' table created")
	}

	return nil
}

func addOptionalColumns(db *gorm.DB) {
	columns := []string{"speed", "accuracy", "altitude", "bearing"}
	for _, col := range columns {
		if !db.Migrator().HasColumn(&LocationPing{}, col) {
			db.Migrator().AddColumn(&LocationPing{}, col)
		}
	}
}

func cleanupOldColumns(db *gorm.DB) error {
	log.Println("🧹 Cleaning up deprecated columns...")

	// Remove old columns from drivers table
	columnsToRemove := []struct {
		model      interface{}
		tableName  string
		columnName string
	}{
		{&Driver{}, "drivers", "current_location_ping_id"},
		{&Driver{}, "drivers", "current_trip_id"},
		{&Driver{}, "drivers", "is_in_trip"},
	}

	for _, col := range columnsToRemove {
		if db.Migrator().HasColumn(col.model, col.columnName) {
			log.Printf("  Removing '%s' from '%s'...", col.columnName, col.tableName)
			if err := db.Migrator().DropColumn(col.model, col.columnName); err != nil {
				log.Printf("  ⚠️  Warning: Failed to drop %s: %v", col.columnName, err)
			} else {
				log.Printf("  ✅ '%s' removed", col.columnName)
			}
		}
	}

	return nil
}

// CreateLegacySessions - Optional: Migrate old location pings to sessions
// Call this manually if you want to preserve historical location data
func CreateLegacySessions(db *gorm.DB) error {
	log.Println("========================================")
	log.Println("Creating Legacy Sessions")
	log.Println("========================================")

	// Check if we have old data to migrate
	var count int64
	db.Raw("SELECT COUNT(*) FROM location_pings WHERE driver_id IS NOT NULL AND (session_id IS NULL OR session_id = 0)").Scan(&count)

	if count == 0 {
		log.Println("✅ No legacy location pings to migrate")
		return nil
	}

	log.Printf("Found %d location pings to migrate", count)

	// Get unique driver/date combinations
	type DaySession struct {
		DriverID uint
		Date     string
	}

	var daySessions []DaySession
	db.Raw(`
		SELECT DISTINCT 
			driver_id,
			DATE(time_stamp) as date
		FROM location_pings
		WHERE driver_id IS NOT NULL AND (session_id IS NULL OR session_id = 0)
		ORDER BY driver_id, date
	`).Scan(&daySessions)

	log.Printf("Creating sessions for %d driver/day combinations", len(daySessions))

	successCount := 0
	errorCount := 0

	for _, ds := range daySessions {
		// Find first car associated with this driver
		var car Car
		db.Where("driver_id = ?", ds.DriverID).First(&car)

		if car.ID == 0 {
			log.Printf("⚠️  No car found for driver %d, skipping...", ds.DriverID)
			errorCount++
			continue
		}

		// Create or get legacy tablet for this car
		var tablet Tablet
		tabletHWID := fmt.Sprintf("LEGACY_CAR_%d", car.ID)

		if err := db.Where("hwid = ?", tabletHWID).First(&tablet).Error; err != nil {
			// Create legacy tablet
			tablet = Tablet{
				HWID:         tabletHWID,
				DeviceName:   "Legacy Migration",
				DeviceType:   "legacy",
				CurrentCarID: &car.ID,
				Status:       "inactive",
			}
			db.Create(&tablet)
		}

		// Get first and last ping for this day
		var firstTime, lastTime time.Time
		db.Raw(`
			SELECT MIN(time_stamp), MAX(time_stamp)
			FROM location_pings
			WHERE driver_id = ? AND DATE(time_stamp) = ?
		`, ds.DriverID, ds.Date).Row().Scan(&firstTime, &lastTime)

		// Create legacy session
		session := DriverSession{
			DriverID:  ds.DriverID,
			CarID:     car.ID,
			TabletID:  tablet.ID,
			StartTime: firstTime,
			EndTime:   &lastTime,
		}

		if err := db.Create(&session).Error; err != nil {
			log.Printf("⚠️  Failed to create session: %v", err)
			Observability.ReportJobFailure("session_migration", "create_session", err)
			errorCount++
			continue
		}

		// Update location pings with session_id
		result := db.Exec(`
			UPDATE location_pings
			SET session_id = ?
			WHERE driver_id = ? AND DATE(time_stamp) = ? AND (session_id IS NULL OR session_id = 0)
		`, session.ID, ds.DriverID, ds.Date)

		if result.Error != nil {
			log.Printf("⚠️  Failed to update pings: %v", result.Error)
			errorCount++
		} else {
			log.Printf("✅ Migrated driver %d on %s (%d pings)",
				ds.DriverID, ds.Date, result.RowsAffected)
			successCount++
		}
	}

	log.Println("========================================")
	log.Printf("Legacy Session Migration Complete")
	log.Printf("✅ Success: %d sessions", successCount)
	log.Printf("❌ Errors: %d sessions", errorCount)
	log.Println("========================================")

	return nil
}
