package testutil

import (
	"Falcon/Models"
	"testing"

	"gorm.io/gorm"
)

// DocumentsSchema is PGSchema with the cars and drivers tables and the
// documents schema (migrated twice, as every boot re-runs it).
func DocumentsSchema(t *testing.T) *gorm.DB {
	t.Helper()
	db := PGSchema(t)
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&Models.Driver{}, &Models.Car{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := Models.MigrateDocuments(db); err != nil {
			t.Fatalf("migrate documents (run %d): %v", i+1, err)
		}
	}
	return db
}
