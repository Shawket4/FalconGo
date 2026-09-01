package Models

import (
	"Falcon/Observability"
	"fmt"
	"log"
	"time"
)

// SetupPerformanceOptimizations creates all performance enhancements
// Call this ONCE after Connect() in main.go
func SetupPerformanceOptimizations() {
	log.Println("⚡ Setting up performance optimizations...")

	// 1. Create materialized views
	if err := createMaterializedViews(); err != nil {
		log.Printf("❌ Failed to create materialized views: %v", err)
		Observability.ReportJobFailure("startup", "create_materialized_views", err)
		log.Println("⚠️  Skipping performance optimizations")
		return
	}

	// 2. Create partial indexes for active sessions
	createPartialIndexes()

	// 3. Setup table partitioning (optional)
	setupPartitioning()

	// 4. Configure PostgreSQL settings
	configurePostgresSettings()

	// 5. Schedule materialized view refresh
	go func() {
		defer Observability.Guard("materialized_view_refresh")
		refreshMaterializedViewsPeriodically()
	}()

	log.Println("✅ Performance optimizations complete")
}

// ============================================================================
// 1. Materialized Views (Cached Query Results)
// ============================================================================

func createMaterializedViews() error {
	log.Println("   Creating materialized views...")

	// Check if location_pings table exists
	var tableExists bool
	if err := DB.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'location_pings')`).Scan(&tableExists).Error; err != nil {
		log.Printf("   ❌ Error checking for location_pings table: %v", err)
		return err
	}
	if !tableExists {
		log.Println("   ⚠️  Table 'location_pings' does not exist yet. Run migrations first.")
		return fmt.Errorf("location_pings table does not exist")
	}

	// Check if driver_sessions table exists
	if err := DB.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'driver_sessions')`).Scan(&tableExists).Error; err != nil {
		log.Printf("   ❌ Error checking for driver_sessions table: %v", err)
		return err
	}
	if !tableExists {
		log.Println("   ⚠️  Table 'driver_sessions' does not exist yet. Run migrations first.")
		return fmt.Errorf("driver_sessions table does not exist")
	}

	// Check if drivers table exists
	if err := DB.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'drivers')`).Scan(&tableExists).Error; err != nil {
		log.Printf("   ❌ Error checking for drivers table: %v", err)
		return err
	}
	if !tableExists {
		log.Println("   ⚠️  Table 'drivers' does not exist yet. Run migrations first.")
		return fmt.Errorf("drivers table does not exist")
	}

	// Check if cars table exists
	if err := DB.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'cars')`).Scan(&tableExists).Error; err != nil {
		log.Printf("   ❌ Error checking for cars table: %v", err)
		return err
	}
	if !tableExists {
		log.Println("   ⚠️  Table 'cars' does not exist yet. Run migrations first.")
		return fmt.Errorf("cars table does not exist")
	}

	// Active sessions with last location (refreshes every 5 min)
	// Drop existing view first
	if err := DB.Exec(`DROP MATERIALIZED VIEW IF EXISTS active_sessions_with_last_location CASCADE`).Error; err != nil {
		log.Printf("   ⚠️  Warning dropping active_sessions_with_last_location: %v", err)
	}

	// Create the materialized view
	if err := DB.Exec(`
		CREATE MATERIALIZED VIEW active_sessions_with_last_location AS
		SELECT 
			ds.id as session_id,
			ds.driver_id,
			ds.car_id,
			ds.tablet_id,
			ds.start_time,
			d.name as driver_name,
			c.car_no_plate as car_plate,
			last_ping.id as last_ping_id,
			last_ping.lat as last_lat,
			last_ping.lng as last_lng,
			last_ping.speed as last_speed,
			last_ping.time_stamp as last_ping_time,
			EXTRACT(EPOCH FROM (NOW() - last_ping.time_stamp))/60 as minutes_since_last_ping
		FROM driver_sessions ds
		LEFT JOIN drivers d ON ds.driver_id = d.id AND d.deleted_at IS NULL
		LEFT JOIN cars c ON ds.car_id = c.id AND c.deleted_at IS NULL
		LEFT JOIN LATERAL (
			SELECT id, lat, lng, speed, time_stamp
			FROM location_pings
			WHERE session_id = ds.id
			  AND deleted_at IS NULL
			ORDER BY time_stamp DESC
			LIMIT 1
		) last_ping ON true
		WHERE ds.end_time IS NULL
		  AND ds.deleted_at IS NULL
	`).Error; err != nil {
		log.Printf("   ❌ Error creating active_sessions_with_last_location: %v", err)
		return err
	}

	// Create unique index on the view
	if err := DB.Exec(`CREATE UNIQUE INDEX ON active_sessions_with_last_location(session_id)`).Error; err != nil {
		log.Printf("   ❌ Error creating index on active_sessions_with_last_location: %v", err)
		return err
	}

	log.Println("   ✅ Created active_sessions_with_last_location")

	// Session summaries (pings count, distance, speed stats)
	// Drop existing view first
	if err := DB.Exec(`DROP MATERIALIZED VIEW IF EXISTS session_location_summaries CASCADE`).Error; err != nil {
		log.Printf("   ⚠️  Warning dropping session_location_summaries: %v", err)
	}

	// Create the materialized view
	if err := DB.Exec(`
		CREATE MATERIALIZED VIEW session_location_summaries AS
		SELECT 
			session_id,
			COUNT(*) as total_pings,
			MIN(time_stamp) as first_ping_time,
			MAX(time_stamp) as last_ping_time,
			AVG(speed) as avg_speed,
			MAX(speed) as max_speed,
			MIN(lat) as min_lat,
			MAX(lat) as max_lat,
			MIN(lng) as min_lng,
			MAX(lng) as max_lng
		FROM location_pings
		WHERE session_id IS NOT NULL
		  AND deleted_at IS NULL
		GROUP BY session_id
	`).Error; err != nil {
		log.Printf("   ❌ Error creating session_location_summaries: %v", err)
		return err
	}

	// Create unique index on the view
	if err := DB.Exec(`CREATE UNIQUE INDEX ON session_location_summaries(session_id)`).Error; err != nil {
		log.Printf("   ❌ Error creating index on session_location_summaries: %v", err)
		return err
	}

	log.Println("   ✅ Created session_location_summaries")

	log.Println("   ✅ All materialized views created successfully")
	return nil
}

// Refresh materialized views periodically
func refreshMaterializedViewsPeriodically() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("   🔄 Refreshing materialized views...")

		// Check if views exist before refreshing
		if !MaterializedViewsExist() {
			log.Println("   ⚠️  Materialized views don't exist. Skipping refresh.")
			continue
		}

		// CONCURRENTLY = non-blocking refresh
		failed := 0
		if err := DB.Exec(`REFRESH MATERIALIZED VIEW CONCURRENTLY active_sessions_with_last_location`).Error; err != nil {
			log.Printf("   ⚠️  Error refreshing active_sessions_with_last_location: %v", err)
			Observability.ReportJobFailure("materialized_view_refresh", "active_sessions_with_last_location", err)
			failed++
		}

		if err := DB.Exec(`REFRESH MATERIALIZED VIEW CONCURRENTLY session_location_summaries`).Error; err != nil {
			log.Printf("   ⚠️  Error refreshing session_location_summaries: %v", err)
			Observability.ReportJobFailure("materialized_view_refresh", "session_location_summaries", err)
			failed++
		}

		// Say what happened. This logged success unconditionally, so a refresh
		// that had been failing for a week announced "✅ refreshed" every five
		// minutes while the dashboard served the same stale numbers.
		if failed > 0 {
			log.Printf("   ⚠️  Materialized view refresh: %d of 2 failed", failed)
		} else {
			log.Println("   ✅ Materialized views refreshed")
		}
	}
}

// ============================================================================
// 2. Partial Indexes (Only Index What Matters)
// ============================================================================

func createPartialIndexes() {
	log.Println("   Creating partial indexes...")

	// Simple index for session_id and time_stamp on all location pings
	// This replaces the complex partial index that PostgreSQL doesn't support
	if err := DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_location_pings_session_time
		ON location_pings(session_id, time_stamp DESC)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		log.Printf("   ⚠️  Warning creating idx_location_pings_session_time: %v", err)
		Observability.ReportJobFailure("startup", "create_index.idx_location_pings_session_time", err)
	} else {
		log.Println("   ✅ Created idx_location_pings_session_time")
	}

	// Index only pings with speed data
	if err := DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_location_pings_with_speed
		ON location_pings(session_id, speed DESC)
		WHERE speed IS NOT NULL AND deleted_at IS NULL
	`).Error; err != nil {
		log.Printf("   ⚠️  Warning creating idx_location_pings_with_speed: %v", err)
		Observability.ReportJobFailure("startup", "create_index.idx_location_pings_with_speed", err)
	} else {
		log.Println("   ✅ Created idx_location_pings_with_speed")
	}

	// Compound index for time-based queries
	if err := DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_location_pings_time_session
		ON location_pings(time_stamp DESC, session_id)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		log.Printf("   ⚠️  Warning creating idx_location_pings_time_session: %v", err)
		Observability.ReportJobFailure("startup", "create_index.idx_location_pings_time_session", err)
	} else {
		log.Println("   ✅ Created idx_location_pings_time_session")
	}

	// Index for session lookups
	if err := DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_location_pings_session
		ON location_pings(session_id)
		WHERE session_id IS NOT NULL AND deleted_at IS NULL
	`).Error; err != nil {
		log.Printf("   ⚠️  Warning creating idx_location_pings_session: %v", err)
		Observability.ReportJobFailure("startup", "create_index.idx_location_pings_session", err)
	} else {
		log.Println("   ✅ Created idx_location_pings_session")
	}

	log.Println("   ✅ Partial indexes created")
}

// ============================================================================
// 3. Table Partitioning (Split Large Tables by Time)
// ============================================================================

func setupPartitioning() {
	log.Println("   Setting up table partitioning...")

	// Check if location_pings is already partitioned
	var isPartitioned bool
	DB.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM pg_partitioned_table 
			WHERE partrelid = 'location_pings'::regclass
		)
	`).Scan(&isPartitioned)

	if isPartitioned {
		log.Println("   ⚠️  Table already partitioned, skipping")
		return
	}

	// Note: Converting existing table to partitioned requires migration
	// This is a template for new installations
	log.Println("   ℹ️  Partitioning requires manual migration for existing data")
	log.Println("   ℹ️  See documentation for migration steps")

	// For new installations, you would use:
	// CREATE TABLE location_pings (...) PARTITION BY RANGE (time_stamp);
	// Then create monthly partitions automatically
}

// ============================================================================
// 4. PostgreSQL Configuration Tuning
// ============================================================================

func configurePostgresSettings() {
	log.Println("   Configuring PostgreSQL settings...")

	// Increase work_mem for this session (helps with sorting/aggregation)
	if err := DB.Exec(`SET work_mem = '256MB'`).Error; err != nil {
		log.Printf("   ⚠️  Warning setting work_mem: %v", err)
	} else {
		log.Println("   ✅ Set work_mem to 256MB")
	}

	// Enable JIT compilation for complex queries
	if err := DB.Exec(`SET jit = on`).Error; err != nil {
		log.Printf("   ⚠️  Warning enabling JIT: %v", err)
	} else {
		log.Println("   ✅ Enabled JIT compilation")
	}

	// Increase effective_cache_size hint
	if err := DB.Exec(`SET effective_cache_size = '4GB'`).Error; err != nil {
		log.Printf("   ⚠️  Warning setting effective_cache_size: %v", err)
	} else {
		log.Println("   ✅ Set effective_cache_size to 4GB")
	}

	log.Println("   ✅ PostgreSQL settings configured")
}

// ============================================================================
// Helper Functions to Use Optimized Views
// ============================================================================

// ActiveSessionWithLocation represents the materialized view
type ActiveSessionWithLocation struct {
	SessionID            uint       `gorm:"column:session_id"`
	DriverID             uint       `gorm:"column:driver_id"`
	CarID                uint       `gorm:"column:car_id"`
	TabletID             uint       `gorm:"column:tablet_id"`
	StartTime            time.Time  `gorm:"column:start_time"`
	DriverName           string     `gorm:"column:driver_name"`
	CarPlate             string     `gorm:"column:car_plate"`
	LastPingID           *uint      `gorm:"column:last_ping_id"`
	LastLat              *float64   `gorm:"column:last_lat"`
	LastLng              *float64   `gorm:"column:last_lng"`
	LastSpeed            *float64   `gorm:"column:last_speed"`
	LastPingTime         *time.Time `gorm:"column:last_ping_time"`
	MinutesSinceLastPing *float64   `gorm:"column:minutes_since_last_ping"`
}

// SessionLocationSummary represents the summary view
type SessionLocationSummary struct {
	SessionID     uint      `gorm:"column:session_id"`
	TotalPings    int       `gorm:"column:total_pings"`
	FirstPingTime time.Time `gorm:"column:first_ping_time"`
	LastPingTime  time.Time `gorm:"column:last_ping_time"`
	AvgSpeed      *float64  `gorm:"column:avg_speed"`
	MaxSpeed      *float64  `gorm:"column:max_speed"`
	MinLat        float64   `gorm:"column:min_lat"`
	MaxLat        float64   `gorm:"column:max_lat"`
	MinLng        float64   `gorm:"column:min_lng"`
	MaxLng        float64   `gorm:"column:max_lng"`
}

// MaterializedViewsExist checks if the materialized views exist
func MaterializedViewsExist() bool {
	var count int64
	err := DB.Raw(`
		SELECT COUNT(*) FROM pg_matviews 
		WHERE matviewname IN ('active_sessions_with_last_location', 'session_location_summaries')
	`).Scan(&count).Error

	return err == nil && count == 2
}

// GetActiveSessionsWithLastLocation - FAST query using materialized view
func GetActiveSessionsWithLastLocation() ([]ActiveSessionWithLocation, error) {
	if !MaterializedViewsExist() {
		return nil, fmt.Errorf("materialized views do not exist")
	}

	var sessions []ActiveSessionWithLocation
	err := DB.Table("active_sessions_with_last_location").Find(&sessions).Error
	return sessions, err
}

// GetSessionLocationSummary - FAST query using materialized view
// Returns nil if no summary exists (not an error - just means no location pings yet)
func GetSessionLocationSummary(sessionID uint) (*SessionLocationSummary, error) {
	if !MaterializedViewsExist() {
		return nil, fmt.Errorf("materialized views do not exist")
	}

	var summary SessionLocationSummary
	err := DB.Table("session_location_summaries").
		Where("session_id = ?", sessionID).
		First(&summary).Error

	// If record not found, return nil (not an error - session might have no pings)
	if err != nil && err.Error() == "record not found" {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &summary, nil
}

// RefreshMaterializedViews - Manually trigger refresh
func RefreshMaterializedViews() error {
	log.Println("   🔄 Manually refreshing materialized views...")

	// Check if views exist before refreshing
	if !MaterializedViewsExist() {
		log.Println("   ⚠️  Materialized views don't exist yet. Skipping refresh.")
		return nil
	}

	if err := DB.Exec(`REFRESH MATERIALIZED VIEW CONCURRENTLY active_sessions_with_last_location`).Error; err != nil {
		log.Printf("   ❌ Error refreshing active_sessions_with_last_location: %v", err)
		return err
	}

	if err := DB.Exec(`REFRESH MATERIALIZED VIEW CONCURRENTLY session_location_summaries`).Error; err != nil {
		log.Printf("   ❌ Error refreshing session_location_summaries: %v", err)
		return err
	}

	log.Println("   ✅ Refresh complete")
	return nil
}
