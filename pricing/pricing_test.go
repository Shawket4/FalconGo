package pricing_test

import (
	"Falcon/Models"
	"Falcon/pricing"
	"Falcon/testutil"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// now is well after the fixture's months (May/June 2025), so a backfill
// freezes both, as it would in production.
var now = time.Date(2026, 10, 25, 12, 0, 0, 0, pricing.Cairo)

func money(v float64) string { return fmt.Sprintf("%.2f", v) }

func fixtureDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.TripSchema(t)
	if err := db.Exec(testutil.ApexRevenueFixture).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

func backfill(t *testing.T, db *gorm.DB) pricing.BackfillResult {
	t.Helper()
	res, err := pricing.Backfill(db, now)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	return res
}

/* ------------------------------------------------------------------------ */
/* (a) Parity with apex-rust                                                 */
/* ------------------------------------------------------------------------ */

// groupLine is one line of apex-rust's golden statistics snapshot, rebuilt
// from what the engine stored.
type groupLine struct {
	Group                  string
	Dist, Rev, Rental, VAT float64
}

// storedStats groups the stored figures the way apex-rust's statistics group
// them: Petrol Arrows by drop-off, Watanya by fee band, TAQA/Petromin by
// terminal (rental from truck_month_charges by terminal).
func storedStats(t *testing.T, db *gorm.DB, company string) []groupLine {
	t.Helper()
	group := "t.terminal"
	switch company {
	case "Petrol Arrows":
		group = "t.drop_off_point"
	case "Watanya":
		group = "CASE WHEN fm.id IS NULL THEN 'Unmapped' ELSE 'Fee ' || fm.fee::int END"
	}
	var rows []groupLine
	err := db.Raw(`
        WITH g AS (
            SELECT `+group+` AS grp,
                   COALESCE(SUM(t.priced_km), 0)::float8    AS km,
                   COALESCE(SUM(t.base_revenue), 0)::float8 AS rev,
                   COALESCE(SUM(t.revenue_vat), 0)::float8  AS vat
              FROM trips t
              LEFT JOIN fee_mappings fm ON fm.company = t.company AND fm.terminal = t.terminal
                   AND fm.drop_off_point = t.drop_off_point AND fm.deleted_at IS NULL
             WHERE t.deleted_at IS NULL AND t.company = ?
             GROUP BY 1
        ),
        r AS (
            SELECT terminal AS grp, SUM(rental)::float8 AS rental, SUM(rental_vat)::float8 AS rvat
              FROM truck_month_charges WHERE company = ? GROUP BY 1
        )
        SELECT g.grp AS "group", g.km AS dist, g.rev AS rev,
               COALESCE(r.rental, 0) AS rental, g.vat + COALESCE(r.rvat, 0) AS vat
          FROM g LEFT JOIN r USING (grp) ORDER BY 1`, company, company).Scan(&rows).Error
	if err != nil {
		t.Fatalf("%s stats: %v", company, err)
	}
	return rows
}

// TestParityWithApexRustGolden: apex-rust's seed_trip_fixture, priced by the
// backfill, stores exactly the figures in apex-rust's revenue_golden.txt.
// Distance for volume-priced companies is not part of the stored basis
// (priced_km is NULL for them), so only per-km distance is compared.
func TestParityWithApexRustGolden(t *testing.T) {
	db := fixtureDB(t)
	res := backfill(t, db)
	// Every live row; the soft-deleted one is not touched.
	if res.Trips != 56 {
		t.Errorf("backfill priced %d trips; want 56 live fixture rows", res.Trips)
	}
	if res.Errors != 2 {
		t.Errorf("backfill left %d unpriced; want 2 (PA-Unmapped, WA-Unmapped)", res.Errors)
	}

	want := map[string][]string{
		// group | dist rev rental vat  (from revenue_golden.txt)
		"Petrol Arrows": {
			"PA-Far | rev=2212.50 rental=0.00 vat=0.00",
			"PA-Near | rev=2318.00 rental=0.00 vat=0.00",
			"PA-Unmapped | rev=0.00 rental=0.00 vat=0.00",
		},
		"Watanya": {
			"Fee 1 | rev=7315.00 rental=0.00 vat=1024.10",
			"Fee 15 | rev=12606.00 rental=0.00 vat=1764.84",
			"Fee 2 | rev=4884.00 rental=0.00 vat=683.76",
			"Unmapped | rev=0.00 rental=0.00 vat=0.00",
		},
		"TAQA": {
			"TQ-T1 | dist=8610.00 rev=434805.00 rental=62964.29 vat=69687.70",
		},
		"Petromin": {
			"PM-T1 | dist=720.00 rev=30600.00 rental=6000.00 vat=5124.00",
		},
	}
	for company, lines := range want {
		got := storedStats(t, db, company)
		var gotLines []string
		for _, g := range got {
			dist := ""
			if company == "TAQA" || company == "Petromin" {
				dist = "dist=" + money(g.Dist) + " "
			}
			gotLines = append(gotLines, fmt.Sprintf("%s | %srev=%s rental=%s vat=%s",
				g.Group, dist, money(g.Rev), money(g.Rental), money(g.VAT)))
		}
		if strings.Join(gotLines, "\n") != strings.Join(lines, "\n") {
			t.Errorf("%s:\n--- stored ---\n%s\n--- apex-rust golden ---\n%s",
				company, strings.Join(gotLines, "\n"), strings.Join(lines, "\n"))
		}
	}

	// with_vat for TAQA, the figure the invoice carries.
	var taqaTotal float64
	db.Raw(`SELECT (SELECT SUM(base_revenue + revenue_vat) FROM trips WHERE company = 'TAQA' AND deleted_at IS NULL)
                  + (SELECT SUM(rental + rental_vat) FROM truck_month_charges WHERE company = 'TAQA')`).Scan(&taqaTotal)
	if money(taqaTotal) != "567456.99" {
		t.Errorf("TAQA with VAT = %s; want 567456.99", money(taqaTotal))
	}

	// The hand-computed truck-months behind 62964.29 and 6000.
	var tms []struct {
		CarNoPlate, Month string
		Days              int
		Rental            float64
	}
	db.Raw(`SELECT car_no_plate, to_char(month, 'YYYY-MM') AS month, month_days_worked AS days, rental::float8 AS rental
              FROM truck_month_charges ORDER BY company, month, car_no_plate`).Scan(&tms)
	var tmLines []string
	for _, r := range tms {
		tmLines = append(tmLines, fmt.Sprintf("%s %s %d %s", r.CarNoPlate, r.Month, r.Days, money(r.Rental)))
	}
	wantTM := []string{
		"PM-A 2025-05 2 4000.00", "PM-B 2025-05 1 2000.00",
		"TQ-A 2025-05 28 43000.00", "TQ-B 2025-05 10 15357.14", "TQ-A 2025-06 3 4607.14",
	}
	if strings.Join(tmLines, "\n") != strings.Join(wantTM, "\n") {
		t.Errorf("truck-months:\n%s\nwant:\n%s", strings.Join(tmLines, "\n"), strings.Join(wantTM, "\n"))
	}

	// Soft-deleted: never priced.
	var deletedPriced int64
	db.Raw(`SELECT COUNT(*) FROM trips WHERE deleted_at IS NOT NULL AND priced_at IS NOT NULL`).Scan(&deletedPriced)
	if deletedPriced != 0 {
		t.Errorf("%d soft-deleted rows were priced", deletedPriced)
	}

	// Both months closed long ago: frozen by the backfill.
	var open int64
	db.Raw(`SELECT COUNT(*) FROM truck_month_charges WHERE frozen_at IS NULL`).Scan(&open)
	if open != 0 || res.Frozen != 3 {
		t.Errorf("open truck-month rows = %d, frozen company-months = %d; want 0 and 3", open, res.Frozen)
	}
}

// apexRustPerRow is apex-rust's allocation::per_row_revenue_cte, transcribed
// from src/db/revenue.rs with its constants substituted (and t.* narrowed to
// the columns it uses), windowed exactly the
// way tests/revenue_parity.rs windows it (company + date range).
const apexRustPerRow = `
WITH scoped AS (
    -- t.* in apex-rust; trips here also carries the stored base_revenue, so
    -- the columns the chain reads are named instead.
    SELECT t.id, t.company, t.car_no_plate, t.date, t.parent_trip_id,
        CASE WHEN ROW_NUMBER() OVER (PARTITION BY COALESCE(NULLIF(t.parent_trip_id, 0), -t.id)
                                     ORDER BY COALESCE(fm.distance, 0.0) DESC, t.id) = 1
             THEN COALESCE(fm.distance, 0.0) ELSE 0.0 END::float8 AS trip_distance,
        CASE t.company
          WHEN 'Petrol Arrows' THEN (t.tank_capacity * COALESCE(fm.fee::float8, 0.0) / 1000.0)::float8
          WHEN 'Watanya' THEN (t.tank_capacity * CASE COALESCE(fm.fee::int, 0)
               WHEN 1 THEN 104.5 WHEN 2 THEN 122.1 WHEN 3 THEN 129.8 WHEN 4 THEN 156.2 WHEN 5 THEN 183.7
               WHEN 6 THEN 196.9 WHEN 7 THEN 210.1 WHEN 8 THEN 235.4 WHEN 9 THEN 261.8 WHEN 10 THEN 288.2
               WHEN 11 THEN 314.6 WHEN 12 THEN 341.0 WHEN 13 THEN 367.4 WHEN 14 THEN 393.8 WHEN 15 THEN 420.2
               ELSE 0.0 END / 1000.0)::float8
          WHEN 'TAQA' THEN (CASE WHEN ROW_NUMBER() OVER (PARTITION BY COALESCE(NULLIF(t.parent_trip_id, 0), -t.id)
                                     ORDER BY COALESCE(fm.distance, 0.0) DESC, t.id) = 1
                                 THEN COALESCE(fm.distance, 0.0) ELSE 0.0 END * 50.5)::float8
          WHEN 'Petromin' THEN (CASE WHEN ROW_NUMBER() OVER (PARTITION BY COALESCE(NULLIF(t.parent_trip_id, 0), -t.id)
                                     ORDER BY COALESCE(fm.distance, 0.0) DESC, t.id) = 1
                                 THEN COALESCE(fm.distance, 0.0) ELSE 0.0 END * 42.5)::float8
          ELSE 0.0 END::float8 AS base_revenue
      FROM trips t
      LEFT JOIN fee_mappings fm ON fm.company = t.company AND fm.terminal = t.terminal
           AND fm.drop_off_point = t.drop_off_point AND fm.deleted_at IS NULL
     WHERE t.deleted_at IS NULL AND (t.company = ? AND t.date BETWEEN ? AND ?)
),
taqa_months AS (
    SELECT car_no_plate, DATE_TRUNC('month', date::date) AS month, COUNT(DISTINCT date)::float8 AS working_days
      FROM scoped WHERE company = 'TAQA' GROUP BY car_no_plate, DATE_TRUNC('month', date::date)
),
allocated AS (
    SELECT s.*,
        CASE s.company
          WHEN 'TAQA' THEN (((COALESCE(CASE
                 WHEN COALESCE(tm.working_days, 0) >= 28 THEN 43000.0
                 WHEN COALESCE(tm.working_days, 0) > 0 THEN GREATEST(0.0, 43000.0 -
                      ((28 - COALESCE(tm.working_days, 0)) * (43000.0 / 28.0)::float8))
                 ELSE 0.0 END::float8, 0.0) / NULLIF(COALESCE(tm.working_days, 0), 0)))
               / COUNT(*) OVER (PARTITION BY s.company, s.car_no_plate, s.date))::float8
          WHEN 'Petromin' THEN ((2000.0) / COUNT(*) OVER (PARTITION BY s.company, s.car_no_plate, s.date))::float8
          ELSE 0.0 END::float8 AS allocated_rental
      FROM scoped s
      LEFT JOIN taqa_months tm ON s.company = 'TAQA' AND tm.car_no_plate = s.car_no_plate
           AND tm.month = DATE_TRUNC('month', s.date::date)
),
priced AS (
    SELECT a.*, CASE WHEN a.company = 'Petrol Arrows' THEN 0.0
                     ELSE ((a.base_revenue + a.allocated_rental) * 0.14)::float8 END::float8 AS allocated_vat
      FROM allocated a
)
SELECT p.id, p.base_revenue, p.allocated_rental, p.allocated_vat,
       (p.base_revenue + p.allocated_rental + p.allocated_vat)::float8 AS allocated_total
  FROM priced p ORDER BY p.id`

type perRow struct {
	ID                                         uint
	BaseRevenue, AllocatedRental, AllocatedVAT float64
	AllocatedTotal                             float64
}

// TestPerRowMatchesApexRustTripsList: every row of the trips list — base
// revenue, allocated rental, allocated VAT, total — read from the stored
// figures through the trip_revenue view equals what apex-rust's per-row CTE
// computes live, to the cent.
func TestPerRowMatchesApexRustTripsList(t *testing.T) {
	db := fixtureDB(t)
	backfill(t, db)

	for _, company := range []string{"Petrol Arrows", "Watanya", "TAQA", "Petromin"} {
		var rust []perRow
		if err := db.Raw(apexRustPerRow, company, "2025-05-01", "2025-06-30").Scan(&rust).Error; err != nil {
			t.Fatalf("apex-rust per-row %s: %v", company, err)
		}
		var stored []perRow
		if err := db.Raw(`
            SELECT trip_id AS id, COALESCE(base_revenue, 0)::float8 AS base_revenue,
                   allocated_rental::float8 AS allocated_rental, allocated_vat::float8 AS allocated_vat,
                   allocated_total::float8 AS allocated_total
              FROM trip_revenue WHERE company = ? AND date BETWEEN ? AND ? ORDER BY trip_id`,
			company, "2025-05-01", "2025-06-30").Scan(&stored).Error; err != nil {
			t.Fatalf("trip_revenue %s: %v", company, err)
		}
		if len(rust) != len(stored) || len(rust) == 0 {
			t.Fatalf("%s: %d apex-rust rows vs %d stored", company, len(rust), len(stored))
		}
		for i := range rust {
			r, s := rust[i], stored[i]
			if r.ID != s.ID || money(r.BaseRevenue) != money(s.BaseRevenue) ||
				money(r.AllocatedRental) != money(s.AllocatedRental) ||
				money(r.AllocatedVAT) != money(s.AllocatedVAT) ||
				money(r.AllocatedTotal) != money(s.AllocatedTotal) {
				t.Errorf("%s row %d: apex-rust %+v, stored %+v", company, r.ID, r, s)
			}
		}
	}

	// Spot-checks by hand: a Petromin car-day shared by two trips, and a
	// tapered TAQA month whose per-day rate is the constant 43000/28.
	type spot struct{ receipt, base, rental, vat string }
	for _, sp := range []spot{
		{"PM1", "7650.00", "1000.00", "1211.00"},
		{"PM3", "7650.00", "2000.00", "1351.00"},
		{"TQB3", "10605.00", "1535.71", "1699.70"},
		{"WAC2", "1045.00", "0.00", "146.30"},
		{"PA1", "1220.00", "0.00", "0.00"},
	} {
		var r perRow
		db.Raw(`SELECT v.base_revenue::float8 AS base_revenue, v.allocated_rental::float8 AS allocated_rental,
                       v.allocated_vat::float8 AS allocated_vat
                  FROM trip_revenue v JOIN trips t ON t.id = v.trip_id WHERE t.receipt_no = ?`, sp.receipt).Scan(&r)
		if got := []string{money(r.BaseRevenue), money(r.AllocatedRental), money(r.AllocatedVAT)}; strings.Join(got, " ") != strings.Join([]string{sp.base, sp.rental, sp.vat}, " ") {
			t.Errorf("%s: base/rental/vat = %v; want %s %s %s", sp.receipt, got, sp.base, sp.rental, sp.vat)
		}
	}
}

// TestPerKmBilledOnceOnTheFurthestDrop: a TAQA trip with two containers to
// different sites bills the furthest once, on the row that holds it.
func TestPerKmBilledOnceOnTheFurthestDrop(t *testing.T) {
	db := testutil.TripSchema(t)
	db.Exec(`INSERT INTO fee_mappings (company, terminal, drop_off_point, distance, fee) VALUES
                ('TAQA','T','Near',100,0), ('TAQA','T','Far',300,0)`)
	db.Exec(`INSERT INTO trips (id, company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no, parent_trip_id) VALUES
                (1,'TAQA','T','Near','C',10000,'2025-05-01','R1',7),
                (2,'TAQA','T','Far','C',10000,'2025-05-01','R2',7),
                (3,'TAQA','T','Far','C',10000,'2025-05-01','R3',7)`)
	var res pricing.PriceResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		res, err = pricing.PriceTrips(tx, []uint{1})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Priced != 3 {
		t.Errorf("priced %d rows; want the whole logical trip (3)", res.Priced)
	}
	var rows []struct {
		ID       uint
		Base, Km float64
	}
	db.Raw(`SELECT id, base_revenue::float8 AS base, priced_km::float8 AS km FROM trips ORDER BY id`).Scan(&rows)
	want := "1:0.00/0.00 2:15150.00/300.00 3:0.00/0.00"
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%d:%s/%s", r.ID, money(r.Base), money(r.Km)))
	}
	if strings.Join(got, " ") != want {
		t.Errorf("rows = %s; want %s", strings.Join(got, " "), want)
	}

	// Delete the billed row: the sibling at the same distance takes over.
	db.Exec(`UPDATE trips SET deleted_at = now() WHERE id = 2`)
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := pricing.PriceTrips(tx, []uint{2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var base3 float64
	db.Raw(`SELECT base_revenue::float8 FROM trips WHERE id = 3`).Scan(&base3)
	if money(base3) != "15150.00" {
		t.Errorf("after deleting the billed container, its sibling bills %s; want 15150.00", money(base3))
	}
}

/* ------------------------------------------------------------------------ */
/* (d) Nothing silently earns                                                */
/* ------------------------------------------------------------------------ */

func TestUnpriceableRowsCarryAnErrorNotRevenue(t *testing.T) {
	db := testutil.TripSchema(t)
	db.Exec(`INSERT INTO fee_mappings (company, terminal, drop_off_point, distance, fee) VALUES
                ('Watanya','W','B17',100,17), ('Mystery Co','M','X',100,50)`)
	db.Exec(`INSERT INTO trips (id, company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no) VALUES
                (1,'Watanya','W','B17','A',30000,'2025-05-01','R1'),
                (2,'Mystery Co','M','X','A',30000,'2025-05-01','R2'),
                (3,'Watanya','W',?,'A',30000,'2025-05-01','R3'),
                (4,'Watanya','W','Nowhere','A',30000,'2025-05-01','R4'),
                (5,'Watanya','W','B17','A',30000,'01/05/2025','R5'),
                (6,'TAQA','T','Site','A',30000,'1999-12-31','R6')`, pricing.UnregisteredDropOff)
	if _, err := pricing.Backfill(db, now); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID           uint
		BaseRevenue  *float64
		Revenue      float64
		PricingError *string
	}
	db.Raw(`SELECT id, base_revenue::float8 AS base_revenue, revenue::float8 AS revenue, pricing_error FROM trips ORDER BY id`).Scan(&rows)
	want := map[uint]string{
		1: "band 17 has no rate in pricing rule",
		2: `no pricing rule for company "Mystery Co" on 2025-05-01`,
		3: "unregistered drop-off",
		4: "no fee mapping for Watanya / W -> Nowhere",
		5: `invalid trip date "01/05/2025"`,
		6: `no pricing rule for company "TAQA" on 1999-12-31`,
	}
	for _, r := range rows {
		if r.PricingError == nil || !strings.Contains(*r.PricingError, want[r.ID]) {
			t.Errorf("trip %d: pricing_error = %v; want it to contain %q", r.ID, r.PricingError, want[r.ID])
		}
		if r.BaseRevenue != nil || r.Revenue != 0 {
			t.Errorf("trip %d: base_revenue = %v, revenue = %v; an unpriced trip earns nothing", r.ID, r.BaseRevenue, r.Revenue)
		}
	}
	// No rule, no rental: an unknown company's car-month is not billed.
	var n int64
	db.Raw(`SELECT COUNT(*) FROM truck_month_charges WHERE company = 'Mystery Co'`).Scan(&n)
	if n != 0 {
		t.Errorf("unknown company got %d truck-month rows", n)
	}
}

/* ------------------------------------------------------------------------ */
/* (b) Effective dates, (e) freeze                                           */
/* ------------------------------------------------------------------------ */

func TestEffectiveDatedRuleAppliesForwardAndViaReprice(t *testing.T) {
	db := fixtureDB(t)
	backfill(t, db)

	vat, rate, amount, full := 0.14, 60.0, 50000.0, 28
	in := pricing.RuleInput{Company: "TAQA", EffectiveFrom: "2025-06-01", Method: Models.PricingPerKm,
		PerKmRate: &rate, VatRate: &vat, RentalMode: Models.RentalMonthlyTaper,
		RentalAmount: &amount, RentalFullDays: &full}
	var ruleID uint
	var stale int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		ruleID, stale, err = pricing.CreateRule(tx, in, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if stale != 3 {
		t.Errorf("unrepriced trips = %d; want the 3 June TAQA rows", stale)
	}
	juneBase := func() string {
		var v float64
		db.Raw(`SELECT SUM(base_revenue)::float8 FROM trips WHERE company = 'TAQA' AND date >= '2025-06-01' AND receipt_no LIKE 'TQAJ%'`).Scan(&v)
		return money(v)
	}
	// Creating the rule moved nothing: the trips keep the price they had.
	if got := juneBase(); got != "31815.00" { // 3 × 210 × 50.5
		t.Errorf("June base after creating the rule = %s; want 31815.00 (unchanged)", got)
	}

	// A new trip dated on/after the start is priced by the new version...
	db.Exec(`INSERT INTO trips (id, company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no)
             VALUES (5000, 'TAQA','TQ-T1','TQ-Site','TQ-A', 30000, '2025-06-10', 'NEW1'),
                    (5001, 'TAQA','TQ-T1','TQ-Site','TQ-A', 30000, '2025-05-30', 'NEW0')`)
	var pr pricing.PriceResult
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		pr, err = pricing.PriceTrips(tx, []uint{5000, 5001})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var newRows []struct {
		ID            uint
		Base          float64
		PricingRuleID uint
	}
	db.Raw(`SELECT id, base_revenue::float8 AS base, pricing_rule_id FROM trips WHERE id IN (5000, 5001) ORDER BY id`).Scan(&newRows)
	if newRows[0].PricingRuleID != ruleID || money(newRows[0].Base) != "12600.00" {
		t.Errorf("trip on 2025-06-10 = %+v; want rule %d at 210 × 60 = 12600.00", newRows[0], ruleID)
	}
	// ...one dated before it by the old one.
	if newRows[1].PricingRuleID == ruleID || money(newRows[1].Base) != "10605.00" {
		t.Errorf("trip on 2025-05-30 = %+v; want the seed rule at 10605.00", newRows[1])
	}
	// (e) June is frozen: its rental did not take the new day.
	if len(pr.Frozen) != 2 || len(pr.Months) != 0 {
		t.Errorf("months recomputed %v, frozen %v; want both months frozen and skipped", pr.Months, pr.Frozen)
	}
	rental := func(month string) string {
		var v float64
		db.Raw(`SELECT COALESCE(SUM(rental), 0)::float8 FROM truck_month_charges WHERE company = 'TAQA' AND month = ?::date`, month).Scan(&v)
		return money(v)
	}
	if got := rental("2025-06-01"); got != "4607.14" {
		t.Errorf("frozen June rental = %s; want 4607.14 untouched", got)
	}

	// Re-price refuses frozen months...
	var ferr *pricing.FrozenError
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := pricing.Reprice(tx, "2025-06-01", "2025-06-30", "TAQA", false)
		return err
	})
	if !asFrozen(err, &ferr) || len(ferr.Months) != 1 || ferr.Months[0] != "2025-06" {
		t.Fatalf("reprice of a frozen month: err = %v; want FrozenError [2025-06]", err)
	}
	// ...but only for the company asked about: June is frozen for TAQA, which
	// must not block re-pricing another company's June.
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := pricing.Reprice(tx, "2025-06-01", "2025-06-30", "Petrol Arrows", false)
		return err
	}); err != nil {
		t.Errorf("Petrol Arrows June reprice refused because of TAQA's frozen June: %v", err)
	}
	// ...and with include_frozen moves the old June trips to the new rule
	// and recomputes June's rental under it: 4 days, 50000 - 24 × 50000/28.
	var rr pricing.RepriceResult
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		rr, err = pricing.Reprice(tx, "2025-06-01", "2025-06-30", "TAQA", true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := juneBase(); got != "37800.00" { // 3 × 210 × 60
		t.Errorf("June base after reprice = %s; want 37800.00", got)
	}
	if got := rental("2025-06-01"); got != "7142.86" {
		t.Errorf("June rental after reprice = %s; want 7142.86", got)
	}
	if got := rental("2025-05-01"); got != money(43000+15357.142857) {
		t.Errorf("May rental = %s; reprice of June must not touch May", got)
	}
	if rr.TripsRepriced != 4 || rr.TripsChanged != 3 {
		t.Errorf("reprice counts = %d repriced, %d changed; want 4 and 3", rr.TripsRepriced, rr.TripsChanged)
	}
	if d := rr.After.BaseRevenue - rr.Before.BaseRevenue; math.Abs(d-3*210*(60-50.5)) > 0.005 {
		t.Errorf("base moved by %.2f; want %.2f", d, 3*210*(60-50.5))
	}
	if money(rr.Before.Rental) != "4607.14" || money(rr.After.Rental) != "7142.86" {
		t.Errorf("rental before/after = %s/%s", money(rr.Before.Rental), money(rr.After.Rental))
	}
	var refrozen int64
	db.Raw(`SELECT COUNT(*) FROM truck_month_charges WHERE month = '2025-06-01' AND frozen_at IS NULL`).Scan(&refrozen)
	if refrozen != 0 {
		t.Errorf("%d June rows left unfrozen after an include_frozen reprice", refrozen)
	}
}

func asFrozen(err error, target **pricing.FrozenError) bool {
	fe, ok := err.(*pricing.FrozenError)
	if ok {
		*target = fe
	}
	return ok
}

func TestFreezeWaitsForTheGraceDays(t *testing.T) {
	db := testutil.TripSchema(t)
	db.Exec(`INSERT INTO fee_mappings (company, terminal, drop_off_point, distance, fee) VALUES ('Petromin','P','S',10,0)`)
	db.Exec(`INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no) VALUES
                ('Petromin','P','S','C',1,'2026-09-30','R1'), ('Petromin','P','S','C',1,'2026-10-01','R2')`)
	if _, err := pricing.Backfill(db, time.Date(2026, 10, 2, 23, 0, 0, 0, pricing.Cairo)); err != nil {
		t.Fatal(err)
	}
	frozenMonths := func() []string {
		var m []string
		db.Raw(`SELECT to_char(month, 'YYYY-MM') FROM truck_month_charges WHERE frozen_at IS NOT NULL ORDER BY 1`).Scan(&m)
		return m
	}
	if got := frozenMonths(); len(got) != 0 {
		t.Errorf("frozen on Oct 2 = %v; September stays open through the 2nd", got)
	}
	// A late September trip still lands while the month is open.
	db.Exec(`INSERT INTO trips (id, company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no) VALUES
                (99, 'Petromin','P','S','C',1,'2026-09-29','R3')`)
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := pricing.PriceTrips(tx, []uint{99})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	frozen, err := pricing.FreezeClosedMonths(db, time.Date(2026, 10, 3, 0, 30, 0, 0, pricing.Cairo))
	if err != nil {
		t.Fatal(err)
	}
	if len(frozen) != 1 || frozen[0].Month != "2026-09-01" {
		t.Errorf("frozen on Oct 3 = %v; want September only", frozen)
	}
	var sep float64
	db.Raw(`SELECT rental::float8 FROM truck_month_charges WHERE month = '2026-09-01'`).Scan(&sep)
	if money(sep) != "4000.00" {
		t.Errorf("September rental = %s; want 2 car-days = 4000.00", money(sep))
	}
	// The nightly recompute leaves the frozen month alone and keeps October.
	db.Exec(`UPDATE trips SET deleted_at = now() WHERE id = 99`)
	if _, err := pricing.RecomputeOpenMonths(db, time.Date(2026, 10, 3, 3, 0, 0, 0, pricing.Cairo)); err != nil {
		t.Fatal(err)
	}
	db.Raw(`SELECT rental::float8 FROM truck_month_charges WHERE month = '2026-09-01'`).Scan(&sep)
	if money(sep) != "4000.00" {
		t.Errorf("frozen September moved to %s", money(sep))
	}

	// Re-running the backfill changes nothing.
	again, err := pricing.Backfill(db, time.Date(2026, 10, 3, 3, 0, 0, 0, pricing.Cairo))
	if err != nil || again.Trips != 0 {
		t.Errorf("second backfill = %+v, %v; want nothing to do", again, err)
	}
}

// TestQuoteIsWhatTheEngineStores: Quote prices a hypothetical drop with the
// engine's own formula, so for every standalone fixture trip it gives exactly
// what the backfill stored for it — base, VAT or pricing error.
func TestQuoteIsWhatTheEngineStores(t *testing.T) {
	db := fixtureDB(t)
	backfill(t, db)
	var rows []struct {
		Company, Terminal, DropOffPoint, Date string
		TankCapacity                          float64
		Base, VAT                             *float64
		PricingError                          *string
	}
	db.Raw(`SELECT company, terminal, drop_off_point, date, tank_capacity::float8 AS tank_capacity,
                   base_revenue::float8 AS base, revenue_vat::float8 AS vat, pricing_error
              FROM trips WHERE deleted_at IS NULL AND COALESCE(parent_trip_id, 0) = 0`).Scan(&rows)
	if len(rows) < 40 {
		t.Fatalf("only %d standalone rows", len(rows))
	}
	f := func(p *float64) string {
		if p == nil {
			return "NULL"
		}
		return money(*p)
	}
	for _, r := range rows {
		quotes, err := pricing.Quote(db, r.Company, r.Terminal, r.Date, r.TankCapacity)
		if err != nil {
			t.Fatal(err)
		}
		var q *pricing.QuoteResult
		for i := range quotes {
			if quotes[i].DropOffPoint == r.DropOffPoint {
				q = &quotes[i]
			}
		}
		if q == nil {
			if r.PricingError == nil {
				t.Errorf("%+v: priced, but no quote for its drop-off", r)
			}
			continue // unmapped: Quote only lists mapped drop-offs
		}
		if f(q.Base) != f(r.Base) || f(q.VAT) != f(r.VAT) || (q.Error == nil) != (r.PricingError == nil) {
			t.Errorf("%s %s -> %s on %s: quote %s / %s, stored %s / %s",
				r.Company, r.Terminal, r.DropOffPoint, r.Date, f(q.Base), f(q.VAT), f(r.Base), f(r.VAT))
		}
	}
	// An empty terminal quotes every terminal's routes, each the same as the
	// per-terminal quote.
	for _, r := range rows[:5] {
		one, _ := pricing.Quote(db, r.Company, r.Terminal, r.Date, r.TankCapacity)
		all, err := pricing.Quote(db, r.Company, "", r.Date, r.TankCapacity)
		if err != nil {
			t.Fatal(err)
		}
		mine := 0
		for _, a := range all {
			if a.Terminal != r.Terminal {
				continue
			}
			mine++
			for _, o := range one {
				if o.DropOffPoint == a.DropOffPoint && (f(o.Base) != f(a.Base) || f(o.VAT) != f(a.VAT)) {
					t.Errorf("%s %s: all-terminal quote %s differs from %s", r.Terminal, a.DropOffPoint, f(a.Base), f(o.Base))
				}
			}
		}
		if mine != len(one) || len(all) < len(one) {
			t.Errorf("%s: %d of %d routes under its terminal in the all-terminal quote", r.Terminal, mine, len(one))
		}
	}
	// A company with no rule is quoted with the engine's own error.
	db.Exec(`INSERT INTO fee_mappings (company, terminal, drop_off_point, distance, fee) VALUES ('Nobody', 'T', 'D', 1, 1)`)
	quotes, _ := pricing.Quote(db, "Nobody", "T", "2025-05-05", 1000)
	if len(quotes) != 1 || quotes[0].Base != nil || quotes[0].Error == nil ||
		!strings.Contains(*quotes[0].Error, "no pricing rule") {
		t.Errorf("unknown company quote = %+v", quotes)
	}
}
