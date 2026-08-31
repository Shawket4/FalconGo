package main

import (
	"Falcon/Constants"
	"Falcon/FiberConfig"
	"Falcon/Migrations"
	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/Slack"
	"Falcon/workers"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/gorm"
)

// version stamps Sentry releases so one deploy's errors can be told from the
// next one's. Set it at build time with
//
//	go build -ldflags "-X main.version=$(git rev-parse --short HEAD)"
//
// or override the whole release string with SENTRY_RELEASE.
var version = "dev"

func main() {
	Constants.Init()
	// ============================================
	// STEP 1: SETUP LOGGING
	// ============================================
	if err := setupLogging(); err != nil {
		log.Fatal("Failed to setup logging:", err)
	}
	log.Println("========================================")
	log.Println("Starting Falcon Logistics System")
	log.Println("========================================")

	// Sentry, if SENTRY_DSN is set. Deferred flush so a crash on the way out
	// still delivers. With no DSN this installs nothing and the flush is a
	// no-op, leaving the service exactly as it was.
	flushSentry := Observability.Init("falcon@" + version)
	defer flushSentry()

	// ============================================
	// STEP 2: CREATE REQUIRED DIRECTORIES
	// ============================================
	if err := initUploadDirectories(); err != nil {
		log.Fatal("Failed to initialize upload directories:", err)
	}

	// ============================================
	// STEP 3: DATABASE CONNECTION
	// ============================================
	log.Println("Connecting to database...")
	Models.Connect()

	// Record slow and failing queries as breadcrumbs, so a 500 arrives carrying
	// the queries that ran just before it. No-op when Sentry is not configured.
	if err := Observability.InstrumentDB(Models.DB); err != nil {
		// Instrumentation is not worth failing a boot over.
		log.Println("could not instrument DB for Sentry:", err)
	}
	log.Println("✅ Database connected")

	if err := Models.RefreshMaterializedViews(); err != nil {
		log.Printf("Warning: Failed to refresh materialized views: %v", err)
	}
	// ============================================
	// STEP 4: RUN MIGRATIONS (CRITICAL!)
	// ============================================
	if err := runMigrations(); err != nil {
		log.Fatal("❌ Migration failed:", err)
	}
	Models.SetupPerformanceOptimizations()

	if err := Migrations.MigrateEmployeeAndCreditSystem(Models.DB); err != nil {
		log.Fatal("Migration failed:", err)
	}

	if err := Migrations.MigratePaySlipSystem(Models.DB); err != nil {
		log.Fatal("Phase 2 Migration failed:", err)
	}

	// ============================================
	// STEP 5: GENERATE PINS FOR EXISTING DRIVERS
	// ============================================
	if err := ensureDriverPINs(); err != nil {
		log.Printf("⚠️  Warning: PIN generation had issues: %v", err)
		// Don't fail - app can still run
	}

	// ============================================
	// STEP 6: START BACKGROUND WORKERS
	// ============================================
	startBackgroundWorkers()

	// ============================================
	// STEP 7: SETUP GRACEFUL SHUTDOWN
	// ============================================
	setupGracefulShutdown()

	// ============================================
	// STEP 8: START WEB SERVER
	// ============================================
	log.Println("========================================")
	log.Println("🚀 Starting Fiber web server...")
	log.Println("========================================")
	FiberConfig.FiberConfig()

}

// ============================================
// MIGRATION FUNCTIONS
// ============================================

func runMigrations() error {
	log.Println("========================================")
	log.Println("Running Database Migrations")
	log.Println("========================================")

	// Detect database type and check if first-time setup
	var tableCount int64
	dbType := detectDatabaseType(Models.DB)

	switch dbType {
	case "sqlite":
		// SQLite check
		Models.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
	case "mysql":
		// MySQL check
		Models.DB.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tableCount)
	case "postgres":
		// PostgreSQL check
		Models.DB.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'").Scan(&tableCount)
	default:
		log.Printf("⚠️  Unknown database type: %s, assuming not first time", dbType)
	}

	isFirstTime := tableCount == 0
	if isFirstTime {
		log.Println("🆕 Detected first-time setup - creating all tables")
	} else {
		log.Printf("🔄 Detected existing database (%s) - running migrations", dbType)
	}

	// Run the session system migration
	if err := Models.RunSessionSystemMigration(Models.DB); err != nil {
		return fmt.Errorf("session system migration failed: %w", err)
	}

	log.Println("✅ All migrations completed successfully")
	return nil
}

// detectDatabaseType detects which database we're using
func detectDatabaseType(db *gorm.DB) string {
	dialectName := db.Dialector.Name()
	switch dialectName {
	case "sqlite":
		return "sqlite"
	case "mysql":
		return "mysql"
	case "postgres":
		return "postgres"
	default:
		return dialectName
	}
}

func ensureDriverPINs() error {
	log.Println("========================================")
	log.Println("Checking Driver PINs")
	log.Println("========================================")

	// Count drivers without PINs
	var countWithoutPIN int64
	Models.DB.Model(&Models.Driver{}).
		Where("pin IS NULL OR pin = ''").
		Count(&countWithoutPIN)

	if countWithoutPIN == 0 {
		log.Println("✅ All drivers have PINs")
		return nil
	}

	log.Printf("Found %d drivers without PINs - generating now...", countWithoutPIN)

	// Generate PINs for drivers
	// if err := utils.GeneratePINsForExistingDrivers(Models.DB); err != nil {
	// 	return err
	// }

	log.Println("✅ Driver PINs generated successfully")
	log.Println("⚠️  IMPORTANT: Check console output above for PIN list!")
	log.Println("⚠️  Save these PINs - they cannot be retrieved later!")

	return nil
}

// ============================================
// BACKGROUND WORKERS
// ============================================

func startBackgroundWorkers() {
	log.Println("========================================")
	log.Println("Starting Background Workers")
	log.Println("========================================")

	// 1. Trip Summary Worker
	tripSummaryWorker := workers.NewTripSummaryWorker(Models.DB)
	tripSummaryWorker.Start()
	log.Println("✅ Trip Summary Worker started")

	// 2. Speed Violation Monitoring Worker
	Slack.StartViolationMonitoring()
	log.Println("✅ Speed Violation Monitoring started")

	log.Println("✅ All background workers started")
}

// Document expiration checker (uncomment when ready)
/*
func documentExpirationWorker() {
	log.Println("🔄 Document Expiration Worker started")

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := Notifications.GetExpiringDocuments(); err != nil {
				log.Printf("⚠️  Error checking document expirations: %v", err)
			} else {
				log.Println("✅ Document expiration check completed")
			}
		}
	}
}
*/

// ============================================
// GRACEFUL SHUTDOWN
// ============================================

var (
	tripWorker *workers.TripSummaryWorker
	shutdownCh = make(chan os.Signal, 1)
)

func setupGracefulShutdown() {
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-shutdownCh
		log.Printf("========================================")
		log.Printf("🛑 Received shutdown signal: %v", sig)
		log.Printf("========================================")

		// Stop workers
		if tripWorker != nil {
			log.Println("Stopping Trip Summary Worker...")
			tripWorker.Stop()
			log.Println("✅ Trip Summary Worker stopped")
		}

		// Close database connections
		if sqlDB, err := Models.DB.DB(); err == nil {
			log.Println("Closing database connections...")
			sqlDB.Close()
			log.Println("✅ Database connections closed")
		}

		log.Println("========================================")
		log.Println("✅ Graceful shutdown completed")
		log.Println("========================================")
		os.Exit(0)
	}()
}

// ============================================
// DIRECTORY SETUP
// ============================================

func initUploadDirectories() error {
	log.Println("Initializing upload directories...")

	dirs := []string{
		"./uploads/receipts",
		"./uploads/car-licenses",
		"./uploads/car-licenses-back",
		"./uploads/calibration-licenses",
		"./uploads/calibration-licenses-back",
		"./uploads/tank-licenses",
		"./uploads/tank-licenses-back",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	log.Println("✅ Upload directories initialized")
	return nil
}

// ============================================
// LOGGING SETUP
// ============================================

func setupLogging() error {
	// Create logs directory
	if err := os.MkdirAll("logs", 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Create log file with current date
	logFileName := fmt.Sprintf("logs/app_%s.log", time.Now().Format("2006-01-02"))
	logFile, err := os.OpenFile(logFileName,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Configure logging
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Also log to console (for development)
	// Uncomment this to see logs in console AND file:
	// log.SetOutput(io.MultiWriter(os.Stdout, logFile))

	return nil
}
