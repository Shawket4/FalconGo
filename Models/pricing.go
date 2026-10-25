package Models

import (
	"time"

	"gorm.io/gorm"
)

// Stored trip pricing.
//
// Trip revenue used to be computed at read time, separately, by every
// statistics query and export — five places that had drifted apart. It is now
// computed once, on write, by the pricing package, from the effective-dated
// rules below, and stored: per trip on trips (base_revenue, revenue_vat,
// priced_km, pricing_rule_id, priced_at, pricing_error) and per truck-month in
// truck_month_charges (car rental and its VAT).
//
// The canonical rules are apex-rust's src/db/revenue.rs; the seed rows below
// are its constants, effective from 2000-01-01, so all history prices exactly
// as apex-rust prices it today.

// Pricing methods: how a trip's own (base) revenue is computed.
const (
	// tank_capacity × fee_mappings.fee / 1000 (fee is EGP per 1,000 L).
	PricingPer1000LFee = "per_1000l_fee"
	// tank_capacity × band_rate[fee_mappings.fee] / 1000 (fee is a band number).
	PricingBandPer1000L = "band_per_1000l"
	// per_km_rate × the logical trip's furthest drop, counted once.
	PricingPerKm = "per_km"
)

// Rental modes: how a company rents the car on top of the trips.
const (
	RentalNone         = "none"
	RentalMonthlyTaper = "monthly_taper" // rental_amount per car-month, tapered below rental_full_days
	RentalPerCarDay    = "per_car_day"   // rental_amount per distinct car-day
)

// PricingRule is one effective-dated version of a company's pricing. Versions
// are append-only: a change is a new row with a later effective_from, and a
// trip is priced by the newest version effective on its date.
type PricingRule struct {
	ID              uint          `json:"id" gorm:"primaryKey"`
	Company         string        `json:"company"`
	EffectiveFrom   string        `json:"effective_from"` // YYYY-MM-DD
	Method          string        `json:"method"`
	PerKmRate       *float64      `json:"per_km_rate"`
	VatRate         float64       `json:"vat_rate"`
	RentalMode      string        `json:"rental_mode"`
	RentalAmount    *float64      `json:"rental_amount"`
	RentalFullDays  *int          `json:"rental_full_days"`
	Notes           string        `json:"notes"`
	CreatedByUserID uint          `json:"created_by_user_id"`
	CreatedAt       time.Time     `json:"created_at"`
	Bands           []PricingBand `json:"bands" gorm:"foreignKey:RuleID"`
}

func (PricingRule) TableName() string { return "pricing_rules" }

// PricingBand is one band's rate for a band_per_1000l rule.
type PricingBand struct {
	ID           uint    `json:"-" gorm:"primaryKey"`
	RuleID       uint    `json:"-"`
	Band         int     `json:"band"`
	RatePer1000L float64 `json:"rate_per_1000l" gorm:"column:rate_per_1000l"`
}

func (PricingBand) TableName() string { return "pricing_bands" }

// TruckMonthCharge is one car's rental for one company, terminal and calendar
// month. Rental is earned per car (TAQA: per car-month, tapered by the days it
// worked; Petromin: per car-day), and split between the terminals the car
// served in proportion to the days each saw, exactly as apex-rust's statistics
// split it. The rows for one car-month therefore sum to that car's rental.
//
// Keyed by car_no_plate, not car_id: apex-rust keys a car by its plate, and
// historical rows do not all carry a car_id. car_id is kept for reference.
type TruckMonthCharge struct {
	ID         uint   `json:"id" gorm:"primaryKey"`
	CarNoPlate string `json:"car_no_plate"`
	CarID      *uint  `json:"car_id"`
	Company    string `json:"company"`
	Terminal   string `json:"terminal"`
	Month      string `json:"month"` // YYYY-MM-01
	// Distinct dates this car worked at this terminal this month.
	DaysWorked int `json:"days_worked"`
	// Distinct dates this car worked for the company this month, across all
	// terminals — the figure TAQA's taper is sized by.
	MonthDaysWorked int `json:"month_days_worked"`
	// This row's share of the car's billable days: a date shared between two
	// terminals is half a day to each. Sums to MonthDaysWorked per car-month.
	ChargeableDays float64    `json:"chargeable_days"`
	Rental         float64    `json:"rental"`
	RentalVAT      float64    `json:"rental_vat" gorm:"column:rental_vat"`
	RuleID         *uint      `json:"rule_id"`
	ComputedAt     time.Time  `json:"computed_at"`
	FrozenAt       *time.Time `json:"frozen_at"`
}

func (TruckMonthCharge) TableName() string { return "truck_month_charges" }

// watanyaBandSeed is apex-rust's WATANYA_BAND_RATES.
const watanyaBandSeed = `
        (1, 104.5), (2, 122.1), (3, 129.8), (4, 156.2), (5, 183.7),
        (6, 196.9), (7, 210.1), (8, 235.4), (9, 261.8), (10, 288.2),
        (11, 314.6), (12, 341.0), (13, 367.4), (14, 393.8), (15, 420.2)`

// MigratePricing creates the pricing tables, the stored-pricing columns on
// trips, the trip_revenue view, and seeds one rule per company from
// apex-rust's constants. Idempotent: it runs on every boot.
func MigratePricing(db *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS pricing_rules (
            id                 bigserial   PRIMARY KEY,
            company            text        NOT NULL,
            effective_from     date        NOT NULL,
            method             text        NOT NULL
                CHECK (method IN ('per_1000l_fee', 'band_per_1000l', 'per_km')),
            per_km_rate        numeric,
            vat_rate           numeric     NOT NULL,
            rental_mode        text        NOT NULL DEFAULT 'none'
                CHECK (rental_mode IN ('none', 'monthly_taper', 'per_car_day')),
            rental_amount      numeric,
            rental_full_days   integer,
            notes              text        NOT NULL DEFAULT '',
            created_by_user_id bigint      NOT NULL DEFAULT 0,
            created_at         timestamptz NOT NULL DEFAULT now(),
            UNIQUE (company, effective_from)
        )`,
		`CREATE TABLE IF NOT EXISTS pricing_bands (
            id             bigserial PRIMARY KEY,
            rule_id        bigint    NOT NULL REFERENCES pricing_rules(id) ON DELETE CASCADE,
            band           integer   NOT NULL,
            rate_per_1000l numeric   NOT NULL,
            UNIQUE (rule_id, band)
        )`,
		`CREATE TABLE IF NOT EXISTS truck_month_charges (
            id                bigserial   PRIMARY KEY,
            car_no_plate      text        NOT NULL,
            car_id            bigint,
            company           text        NOT NULL,
            terminal          text        NOT NULL,
            month             date        NOT NULL,
            days_worked       integer     NOT NULL,
            month_days_worked integer     NOT NULL,
            chargeable_days   numeric     NOT NULL,
            rental            numeric     NOT NULL,
            rental_vat        numeric     NOT NULL,
            rule_id           bigint      REFERENCES pricing_rules(id),
            computed_at       timestamptz NOT NULL DEFAULT now(),
            frozen_at         timestamptz,
            UNIQUE (car_no_plate, company, terminal, month)
        )`,
		`CREATE INDEX IF NOT EXISTS idx_truck_month_charges_month
            ON truck_month_charges (month, company)`,
		// One row per finished one-shot job (the stored-pricing backfill).
		`CREATE TABLE IF NOT EXISTS pricing_jobs (
            name        text        PRIMARY KEY,
            finished_at timestamptz NOT NULL DEFAULT now(),
            detail      text        NOT NULL DEFAULT ''
        )`,

		// Stored pricing on trips. AutoMigrate adds these from the struct too;
		// they are repeated here because a failing phase-8 AutoMigrate skips
		// the rest of the phase silently.
		`ALTER TABLE trips ADD COLUMN IF NOT EXISTS base_revenue numeric`,
		`ALTER TABLE trips ADD COLUMN IF NOT EXISTS revenue_vat numeric`,
		`ALTER TABLE trips ADD COLUMN IF NOT EXISTS priced_km numeric`,
		`ALTER TABLE trips ADD COLUMN IF NOT EXISTS pricing_rule_id bigint`,
		`ALTER TABLE trips ADD COLUMN IF NOT EXISTS priced_at timestamptz`,
		`ALTER TABLE trips ADD COLUMN IF NOT EXISTS pricing_error text`,
		`DO $$ BEGIN
            IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_trips_pricing_rule_id') THEN
                ALTER TABLE trips ADD CONSTRAINT fk_trips_pricing_rule_id
                    FOREIGN KEY (pricing_rule_id) REFERENCES pricing_rules(id);
            END IF;
        END $$`,
		// The engine's working sets: a logical trip, and a company-month.
		`CREATE INDEX IF NOT EXISTS idx_trips_logical_trip
            ON trips ((COALESCE(NULLIF(parent_trip_id, 0), -id)))`,
		`CREATE INDEX IF NOT EXISTS idx_trips_company_date
            ON trips (company, date) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_trips_unpriced
            ON trips (date) WHERE deleted_at IS NULL AND pricing_error IS NOT NULL`,

		// The seed: apex-rust's constants, effective for all history.
		`INSERT INTO pricing_rules
            (company, effective_from, method, per_km_rate, vat_rate,
             rental_mode, rental_amount, rental_full_days, notes)
         VALUES
            ('Petrol Arrows', '2000-01-01', 'per_1000l_fee', NULL, 0,
             'none', NULL, NULL, 'seed: apex-rust revenue.rs (fee = EGP per 1,000 L, no VAT)'),
            ('Watanya', '2000-01-01', 'band_per_1000l', NULL, 0.14,
             'none', NULL, NULL, 'seed: apex-rust revenue.rs WATANYA_BAND_RATES'),
            ('TAQA', '2000-01-01', 'per_km', 50.5, 0.14,
             'monthly_taper', 43000, 28, 'seed: apex-rust revenue.rs TAQA_*'),
            ('Petromin', '2000-01-01', 'per_km', 42.5, 0.14,
             'per_car_day', 2000, NULL, 'seed: apex-rust revenue.rs PETROMIN_*')
         ON CONFLICT (company, effective_from) DO NOTHING`,
		`INSERT INTO pricing_bands (rule_id, band, rate_per_1000l)
         SELECT r.id, b.band, b.rate
           FROM pricing_rules r
           CROSS JOIN (VALUES` + watanyaBandSeed + `) AS b(band, rate)
          WHERE r.company = 'Watanya' AND r.effective_from = '2000-01-01'
         ON CONFLICT (rule_id, band) DO NOTHING`,

		tripRevenueView,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}

// tripRevenueView is the per-row breakdown the trips list shows, read from the
// stored figures: a row's own base revenue and VAT, plus its allocated share
// of car rental, allocated the way apex-rust's per_row_revenue_cte allocates
// it — the car-month's rental per day worked, split equally between that
// car's rows on the day. Summed over whole months it equals
// truck_month_charges exactly.
//
// Filter it by date and company (partition columns, so the predicate is
// pushed below the window). Unlike apex-rust's CTE the rental is always the
// whole month's, so a row's share does not move with the caller's range.
const tripRevenueView = `
        CREATE OR REPLACE VIEW trip_revenue AS
        WITH car_months AS (
            SELECT car_no_plate, company, to_char(month, 'YYYY-MM') AS ym,
                   SUM(rental)::numeric     AS rental,
                   SUM(rental_vat)::numeric AS rental_vat,
                   MAX(month_days_worked)   AS month_days
              FROM truck_month_charges
             GROUP BY car_no_plate, company, to_char(month, 'YYYY-MM')
        ),
        rows AS (
            SELECT t.id, t.company, t.date, t.car_no_plate, t.terminal,
                   t.base_revenue, t.revenue_vat, t.priced_km, t.pricing_error,
                   COUNT(*) OVER (PARTITION BY t.company, t.car_no_plate, t.date) AS rows_that_day
              FROM trips t
             WHERE t.deleted_at IS NULL
        )
        SELECT r.id AS trip_id, r.company, r.date, r.car_no_plate, r.terminal,
               r.base_revenue, r.revenue_vat, r.priced_km, r.pricing_error,
               COALESCE(cm.rental     / NULLIF(cm.month_days, 0) / r.rows_that_day, 0) AS allocated_rental,
               COALESCE(r.revenue_vat, 0)
                 + COALESCE(cm.rental_vat / NULLIF(cm.month_days, 0) / r.rows_that_day, 0) AS allocated_vat,
               COALESCE(r.base_revenue, 0)
                 + COALESCE(cm.rental     / NULLIF(cm.month_days, 0) / r.rows_that_day, 0)
                 + COALESCE(r.revenue_vat, 0)
                 + COALESCE(cm.rental_vat / NULLIF(cm.month_days, 0) / r.rows_that_day, 0) AS allocated_total
          FROM rows r
          LEFT JOIN car_months cm
            ON cm.car_no_plate = r.car_no_plate
           AND cm.company      = r.company
           AND cm.ym           = left(r.date, 7)`
