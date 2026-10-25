// Package testutil holds helpers shared by the database-backed tests.
package testutil

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PGSchema opens FALCON_TEST_DSN (a local, throwaway Postgres, e.g.
// postgres://apex:apex@127.0.0.1:5432/falcon_test?sslmode=disable) inside a
// fresh schema of its own, dropped when the test ends, so nothing outside it
// is ever touched. The test is skipped when the variable is unset, which keeps
// the normal run hermetic.
func PGSchema(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("FALCON_TEST_DSN")
	if dsn == "" {
		t.Skip("set FALCON_TEST_DSN to a throwaway Postgres to run the database tests")
	}
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := "falcon_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		if sqlDB, err := admin.DB(); err == nil {
			sqlDB.Close()
		}
	})

	sep := " search_path="
	if strings.Contains(dsn, "://") {
		sep = "&search_path="
		if !strings.Contains(dsn, "?") {
			sep = "?search_path="
		}
	}
	db, err := gorm.Open(postgres.Open(dsn+sep+schema), cfg)
	if err != nil {
		t.Fatalf("open in schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}
