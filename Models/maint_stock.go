package Models

import (
	"time"

	"gorm.io/gorm"
)

// The four movement kinds maint-rust records. Debits come from its oil changes
// and tire mounts, credits from its dashboard; all of them arrive here through
// the same mirror route, so this ledger holds both and on-hand is credits
// minus debits.
const (
	MaintStockTireDebit  = "tire_debit"
	MaintStockOilDebit   = "oil_debit"
	MaintStockTireCredit = "tire_credit"
	MaintStockOilCredit  = "oil_credit"
)

// MaintStockMovement is one stock movement mirrored from maint-rust: the
// authoritative ledger its tire and oil stock caches are refreshed from.
// RefID is maint-rust's id for the consuming (or crediting) event and the
// idempotency key: a replayed mirror inserts nothing.
//
// The table is created by MigrateMaintStockMovements, not AutoMigrate, so its
// uuid columns and CHECK constraint are exactly what the SQL says.
type MaintStockMovement struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	CreatedAt        time.Time `json:"created_at"`
	RefID            string    `json:"ref_id"`
	Kind             string    `json:"kind"`
	Brand            *string   `json:"brand"`
	Model            *string   `json:"model"`
	Size             *string   `json:"size"`
	OilType          *string   `json:"oil_type"`
	Qty              *int      `json:"qty"`
	Liters           *float64  `json:"liters"`
	WorkOrderID      *string   `json:"work_order_id"`
	RecordedByUserID uint      `json:"recorded_by_user_id"`
}

func (MaintStockMovement) TableName() string {
	return "maint_stock_movements"
}

// MigrateMaintStockMovements creates the maint-rust stock ledger. Idempotent.
func MigrateMaintStockMovements(db *gorm.DB) error {
	return db.Exec(`
        CREATE TABLE IF NOT EXISTS maint_stock_movements (
            id                  bigserial   PRIMARY KEY,
            created_at          timestamptz NOT NULL DEFAULT now(),
            ref_id              uuid        NOT NULL UNIQUE,
            kind                text        NOT NULL
                CHECK (kind IN ('tire_debit', 'oil_debit', 'tire_credit', 'oil_credit')),
            brand               text,
            model               text,
            size                text,
            oil_type            text,
            qty                 integer,
            liters              numeric,
            work_order_id       uuid,
            recorded_by_user_id bigint      NOT NULL DEFAULT 0
        )`).Error
}

// RecordMaintStockMovement inserts m unless its RefID is already recorded.
// It reports whether a row was inserted; a replay is not an error.
func RecordMaintStockMovement(db *gorm.DB, m MaintStockMovement) (bool, error) {
	res := db.Exec(`
        INSERT INTO maint_stock_movements
            (ref_id, kind, brand, model, size, oil_type, qty, liters, work_order_id, recorded_by_user_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (ref_id) DO NOTHING`,
		m.RefID, m.Kind, m.Brand, m.Model, m.Size, m.OilType, m.Qty, m.Liters, m.WorkOrderID, m.RecordedByUserID)
	return res.RowsAffected == 1, res.Error
}

// MaintTireOnHand is one tire line's count: credits minus debits.
type MaintTireOnHand struct {
	Brand     string `json:"brand"`
	Model     string `json:"model"`
	Size      string `json:"size"`
	OnHandQty int64  `json:"on_hand_qty"`
}

// MaintOilOnHand is one oil type's liters: credits minus debits.
type MaintOilOnHand struct {
	OilType      string  `json:"oil_type"`
	LitersOnHand float64 `json:"liters_on_hand"`
}

// MaintTireStock computes tire on-hand from the ledger, keyed the way
// maint-rust's tire_stock_cache is: (brand, model, size), with a missing part
// as "". May be negative: an offline oversell is a count to correct, not a
// failure.
func MaintTireStock(db *gorm.DB) ([]MaintTireOnHand, error) {
	rows := []MaintTireOnHand{}
	err := db.Raw(`
        SELECT COALESCE(brand, '') AS brand,
               COALESCE(model, '') AS model,
               COALESCE(size, '')  AS size,
               COALESCE(SUM(CASE WHEN kind = 'tire_credit' THEN qty ELSE -qty END), 0)::bigint AS on_hand_qty
          FROM maint_stock_movements
         WHERE kind IN ('tire_credit', 'tire_debit')
         GROUP BY 1, 2, 3
         ORDER BY 1, 2, 3`).Scan(&rows).Error
	return rows, err
}

// MaintOilStock computes liters on hand per oil type from the ledger.
func MaintOilStock(db *gorm.DB) ([]MaintOilOnHand, error) {
	rows := []MaintOilOnHand{}
	err := db.Raw(`
        SELECT COALESCE(oil_type, '') AS oil_type,
               COALESCE(SUM(CASE WHEN kind = 'oil_credit' THEN liters ELSE -liters END), 0)::float8 AS liters_on_hand
          FROM maint_stock_movements
         WHERE kind IN ('oil_credit', 'oil_debit')
         GROUP BY 1
         ORDER BY 1`).Scan(&rows).Error
	return rows, err
}
