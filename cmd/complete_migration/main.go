package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Setup logging
	logFile, err := os.OpenFile("migration_all_force.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatal("Failed to create log file:", err)
	}
	defer logFile.Close()
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)

	log.Println("========================================")
	log.Println("COMPLETE FORCE MIGRATION")
	log.Println("Migrating ALL tables and ALL rows")
	log.Println("Including duplicates and orphaned data")
	log.Println("========================================\n")

	startTime := time.Now()

	// Connect to SQLite
	log.Println("Step 1: Connecting to SQLite...")
	sqliteDB, err := sql.Open("sqlite3", "database.db")
	if err != nil {
		log.Fatal("SQLite connection failed:", err)
	}
	defer sqliteDB.Close()
	log.Println("✅ Connected to SQLite")

	// Connect to PostgreSQL
	log.Println("\nStep 2: Connecting to PostgreSQL...")
	pgConnStr := "host=localhost port=5432 user=postgres password=REDACTED dbname=apex sslmode=disable"
	postgresDB, err := sql.Open("postgres", pgConnStr)
	if err != nil {
		log.Fatal("PostgreSQL connection failed:", err)
	}
	defer postgresDB.Close()
	log.Println("✅ Connected to PostgreSQL")

	// Drop and recreate schema
	log.Println("\nStep 3: Dropping and recreating schema...")
	_, err = postgresDB.Exec("DROP SCHEMA IF EXISTS public CASCADE")
	if err != nil {
		log.Fatal("Failed to drop schema:", err)
	}
	_, err = postgresDB.Exec("CREATE SCHEMA public")
	if err != nil {
		log.Fatal("Failed to create schema:", err)
	}
	_, err = postgresDB.Exec("GRANT ALL ON SCHEMA public TO shawket")
	if err != nil {
		log.Fatal("Failed to grant permissions:", err)
	}
	log.Println("✅ Schema reset complete")

	// Get all tables from SQLite
	log.Println("\nStep 4: Discovering all tables...")
	rows, err := sqliteDB.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		log.Fatal("Failed to get tables:", err)
	}

	var allTables []string
	for rows.Next() {
		var tableName string
		rows.Scan(&tableName)
		allTables = append(allTables, tableName)
	}
	rows.Close()

	log.Printf("Found %d tables\n", len(allTables))

	// Step 5: Create tables in PostgreSQL and migrate data
	log.Println("\nStep 5: Creating tables and migrating data...")
	log.Println("========================================\n")

	stats := &MigrationStats{
		TableStats: make(map[string]*TableStat),
	}

	for i, tableName := range allTables {
		log.Printf("[%d/%d] Processing: %s", i+1, len(allTables), tableName)

		tableStat := &TableStat{Name: tableName}
		stats.TableStats[tableName] = tableStat

		// Get row count
		var count int64
		err := sqliteDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
		if err != nil {
			log.Printf("    ❌ Failed to count rows: %v\n", err)
			tableStat.Status = "failed"
			tableStat.Error = err.Error()
			continue
		}

		tableStat.SourceRows = count

		if count == 0 {
			log.Printf("    ⚪ Empty - skipping\n")
			tableStat.Status = "empty"
			continue
		}

		log.Printf("    Found %d rows", count)

		// Get table schema from SQLite
		schemaRows, err := sqliteDB.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
		if err != nil {
			log.Printf("    ❌ Failed to get schema: %v\n", err)
			tableStat.Status = "failed"
			tableStat.Error = err.Error()
			continue
		}

		var columns []ColumnInfo
		for schemaRows.Next() {
			var cid int
			var name, dataType string
			var notNull, pk int
			var dfltValue sql.NullString

			schemaRows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk)
			columns = append(columns, ColumnInfo{
				Name:     name,
				DataType: dataType,
				NotNull:  notNull == 1,
				PK:       pk > 0,
			})
		}
		schemaRows.Close()

		// Create table in PostgreSQL
		createSQL := generateCreateTableSQL(tableName, columns)
		_, err = postgresDB.Exec(createSQL)
		if err != nil {
			log.Printf("    ❌ Failed to create table: %v\n", err)
			tableStat.Status = "failed"
			tableStat.Error = err.Error()
			continue
		}

		// Disable constraints temporarily
		postgresDB.Exec(fmt.Sprintf("ALTER TABLE %s DISABLE TRIGGER ALL", tableName))

		// Read and migrate data
		columnNames := make([]string, len(columns))
		for i, col := range columns {
			columnNames[i] = col.Name
		}

		selectSQL := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columnNames, ", "), tableName)
		dataRows, err := sqliteDB.Query(selectSQL)
		if err != nil {
			log.Printf("    ❌ Failed to read data: %v\n", err)
			tableStat.Status = "failed"
			tableStat.Error = err.Error()
			continue
		}

		// Prepare column values
		columnPointers := make([]interface{}, len(columns))
		columnValues := make([]interface{}, len(columns))
		for i := range columnValues {
			columnPointers[i] = &columnValues[i]
		}

		migratedCount := 0
		errorCount := 0

		// Build insert statement
		placeholders := make([]string, len(columns))
		for i := range placeholders {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}
		insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			tableName,
			strings.Join(columnNames, ", "),
			strings.Join(placeholders, ", "))

		for dataRows.Next() {
			err := dataRows.Scan(columnPointers...)
			if err != nil {
				errorCount++
				continue
			}

			// Convert SQLite values to PostgreSQL compatible values
			pgValues := make([]interface{}, len(columnValues))
			for i, val := range columnValues {
				pgValues[i] = convertValue(val, columns[i].DataType)
			}

			// Insert into PostgreSQL
			_, err = postgresDB.Exec(insertSQL, pgValues...)
			if err != nil {
				// Log error but continue
				errorCount++
			} else {
				migratedCount++
			}
		}
		dataRows.Close()

		// Re-enable constraints
		postgresDB.Exec(fmt.Sprintf("ALTER TABLE %s ENABLE TRIGGER ALL", tableName))

		tableStat.MigratedRows = int64(migratedCount)
		tableStat.ErrorRows = int64(errorCount)

		if errorCount > 0 {
			log.Printf("    ⚠️  Migrated %d rows, %d errors\n", migratedCount, errorCount)
			tableStat.Status = "partial"
		} else {
			log.Printf("    ✅ Migrated %d rows\n", migratedCount)
			tableStat.Status = "success"
		}

		stats.TotalMigrated += int64(migratedCount)
		stats.TotalErrors += int64(errorCount)
	}

	// Print summary
	duration := time.Since(startTime)
	log.Println("\n========================================")
	log.Println("MIGRATION SUMMARY")
	log.Println("========================================")
	log.Printf("Total tables:          %d", len(allTables))

	successCount := 0
	partialCount := 0
	failedCount := 0
	emptyCount := 0

	for _, stat := range stats.TableStats {
		switch stat.Status {
		case "success":
			successCount++
		case "partial":
			partialCount++
		case "failed":
			failedCount++
		case "empty":
			emptyCount++
		}
	}

	log.Printf("✅ Fully successful:    %d", successCount)
	log.Printf("⚠️  Partial success:    %d", partialCount)
	log.Printf("❌ Failed:              %d", failedCount)
	log.Printf("⚪ Empty:               %d", emptyCount)
	log.Println("----------------------------------------")
	log.Printf("📊 Total rows migrated: %d", stats.TotalMigrated)
	log.Printf("⚠️  Total errors:       %d", stats.TotalErrors)
	log.Printf("⏱️  Duration:           %s", duration.Round(time.Millisecond))
	log.Println("========================================\n")

	// Detailed table breakdown
	log.Println("Detailed Table Breakdown:")
	log.Println("========================================")
	for _, tableName := range allTables {
		stat := stats.TableStats[tableName]
		statusIcon := "❓"
		switch stat.Status {
		case "success":
			statusIcon = "✅"
		case "partial":
			statusIcon = "⚠️"
		case "failed":
			statusIcon = "❌"
		case "empty":
			statusIcon = "⚪"
		}

		if stat.Status == "empty" {
			log.Printf("%s %-30s: empty", statusIcon, tableName)
		} else if stat.Status == "failed" {
			log.Printf("%s %-30s: failed - %s", statusIcon, tableName, stat.Error)
		} else {
			log.Printf("%s %-30s: %6d/%6d rows (errors: %d)",
				statusIcon, tableName, stat.MigratedRows, stat.SourceRows, stat.ErrorRows)
		}
	}
	log.Println("========================================\n")

	log.Println("✅ MIGRATION COMPLETE!")
	log.Println("\n📝 Log saved to: migration_all_force.log")
	log.Println("\n⚠️  Note: This migration includes ALL data")
	log.Println("   including duplicates and orphaned records.")
	log.Println("   You may need to clean up data integrity issues.")
}

// ============================================================================
// Helper Types and Functions
// ============================================================================

type MigrationStats struct {
	TotalMigrated int64
	TotalErrors   int64
	TableStats    map[string]*TableStat
}

type TableStat struct {
	Name         string
	SourceRows   int64
	MigratedRows int64
	ErrorRows    int64
	Status       string // success, partial, failed, empty
	Error        string
}

type ColumnInfo struct {
	Name     string
	DataType string
	NotNull  bool
	PK       bool
}

func generateCreateTableSQL(tableName string, columns []ColumnInfo) string {
	var columnDefs []string
	var pkColumns []string

	for _, col := range columns {
		pgType := sqliteTypeToPostgres(col.DataType)
		colDef := fmt.Sprintf("%s %s", col.Name, pgType)

		if col.NotNull && !col.PK {
			colDef += " NOT NULL"
		}

		columnDefs = append(columnDefs, colDef)

		if col.PK {
			pkColumns = append(pkColumns, col.Name)
		}
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s",
		tableName,
		strings.Join(columnDefs, ",\n  "))

	if len(pkColumns) > 0 {
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(pkColumns, ", "))
	}

	sql += "\n)"
	return sql
}

func sqliteTypeToPostgres(sqliteType string) string {
	sqliteType = strings.ToUpper(sqliteType)

	// Handle common SQLite types
	switch {
	case strings.Contains(sqliteType, "INT"):
		return "INTEGER"
	case strings.Contains(sqliteType, "CHAR") || strings.Contains(sqliteType, "CLOB") || strings.Contains(sqliteType, "TEXT"):
		return "TEXT"
	case strings.Contains(sqliteType, "BLOB"):
		return "BYTEA"
	case strings.Contains(sqliteType, "REAL") || strings.Contains(sqliteType, "FLOA") || strings.Contains(sqliteType, "DOUB"):
		return "DOUBLE PRECISION"
	case strings.Contains(sqliteType, "DECIMAL") || strings.Contains(sqliteType, "NUMERIC"):
		return "NUMERIC"
	case strings.Contains(sqliteType, "BOOL"):
		return "BOOLEAN"
	case strings.Contains(sqliteType, "DATE"):
		return "TIMESTAMP"
	case strings.Contains(sqliteType, "TIME"):
		return "TIMESTAMP"
	default:
		return "TEXT" // Default fallback
	}
}

func convertValue(value interface{}, dataType string) interface{} {
	if value == nil {
		return nil
	}

	// Handle byte arrays (for BLOB/BYTEA)
	if b, ok := value.([]byte); ok {
		// If it's a datetime string, try to parse it
		str := string(b)
		if strings.Contains(strings.ToUpper(dataType), "DATE") || strings.Contains(strings.ToUpper(dataType), "TIME") {
			// Try to parse various datetime formats
			formats := []string{
				"2006-01-02 15:04:05",
				"2006-01-02T15:04:05Z",
				"2006-01-02T15:04:05",
				"2006-01-02",
				time.RFC3339,
			}
			for _, format := range formats {
				if t, err := time.Parse(format, str); err == nil {
					return t
				}
			}
		}
		return str
	}

	return value
}
