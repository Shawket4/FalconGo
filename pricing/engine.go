// Package pricing computes trip revenue once, on write, and stores it.
//
// The rules are apex-rust's (src/db/revenue.rs), held as effective-dated rows
// in pricing_rules / pricing_bands rather than as constants. This package is
// the only writer of trips.base_revenue, revenue_vat, priced_km,
// pricing_rule_id, priced_at, pricing_error and revenue, and of
// truck_month_charges.
//
// # Two kinds of revenue
//
// Base revenue is per row: each trip earns it from its own volume or distance
// (PriceTrips). Car rental is not: TAQA's is per car per calendar month,
// tapered by the days the car worked; Petromin's is per car-day. It is stored
// per truck-month (RecomputeMonths), split between terminals by the days each
// saw, exactly as apex-rust's statistics split it.
//
// # Per-km attribution
//
// One truck makes one journey however many containers it carries, so a
// per-km logical trip (a parent trip, or a standalone row) is billed once, on
// its furthest drop: the row holding the furthest mapped distance (ties to
// the lowest id) gets distance × rate and priced_km = that distance; its
// siblings get 0 and priced_km = 0. This is apex-rust's trip_distance_sql:
// ROW_NUMBER() OVER (PARTITION BY COALESCE(NULLIF(parent_trip_id,0), -id)
// ORDER BY COALESCE(distance,0) DESC, id).
//
// Everything runs inside the caller's transaction.
package pricing

import (
	"fmt"
	"sort"
	"time"

	"Falcon/Models"

	"gorm.io/gorm"
)

// UnregisteredDropOff is the placeholder drop-off for a trip whose real
// destination is not mapped yet. It has no fee mapping, so it cannot be priced.
const UnregisteredDropOff = "غير مسجل"

// validDate is the shape of a priceable trips.date (TEXT, YYYY-MM-DD). Trip
// dates are never cast to date — ISO dates compare and truncate correctly as
// text — so a malformed value becomes a pricing_error on its own row instead
// of aborting a batch.
const validDate = `'^\d{4}-\d{2}-\d{2}$'`

// MonthRef names one company's calendar month: the unit truck-month rental is
// computed and frozen in. Month is the first day, YYYY-MM-01.
type MonthRef struct {
	Company string `json:"company"`
	Month   string `json:"month"`
}

// MonthOf is the month a trip of company on date (YYYY-MM-DD) belongs to. It
// returns false for a date that is not YYYY-MM-DD.
func MonthOf(company, date string) (MonthRef, bool) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return MonthRef{}, false
	}
	return MonthRef{Company: company, Month: t.Format("2006-01") + "-01"}, true
}

// PriceResult counts what a pricing pass did.
type PriceResult struct {
	Priced  int64      `json:"priced"`  // live rows written
	Changed int64      `json:"changed"` // of those, rows whose base revenue or error moved
	Errors  int64      `json:"errors"`  // of those, rows left unpriced with a pricing_error
	Months  []MonthRef `json:"months"`  // company-months whose rental was recomputed
	Frozen  []MonthRef `json:"frozen"`  // company-months skipped because they are frozen
}

func (r *PriceResult) add(o PriceResult) {
	r.Priced += o.Priced
	r.Changed += o.Changed
	r.Errors += o.Errors
	r.Months = append(r.Months, o.Months...)
	r.Frozen = append(r.Frozen, o.Frozen...)
}

// PriceTrips prices the logical trips containing ids — every live container
// of their parents, since one container's change can move which sibling holds
// the furthest drop — and recomputes the truck-months they fall in. ids may
// include soft-deleted rows: their siblings are re-priced and their month
// recomputed without them. extra names company-months that must also be
// recomputed, typically the month a trip was moved OUT of.
func PriceTrips(tx *gorm.DB, ids []uint, extra ...MonthRef) (PriceResult, error) {
	if len(ids) == 0 && len(extra) == 0 {
		return PriceResult{}, nil
	}
	var res PriceResult
	if len(ids) > 0 {
		var err error
		res, err = priceSeed(tx, `SELECT id FROM trips WHERE id IN ?`, ids)
		if err != nil {
			return res, err
		}
		months, err := monthsOfGroups(tx, `SELECT id FROM trips WHERE id IN ?`, ids)
		if err != nil {
			return res, err
		}
		extra = append(extra, months...)
	}
	recomputed, frozen, err := RecomputeMonths(tx, extra, false)
	res.Months, res.Frozen = recomputed, frozen
	return res, err
}

// PriceParent prices every container of a parent trip (live or deleted) and
// recomputes their months, plus extra.
func PriceParent(tx *gorm.DB, parentID uint, extra ...MonthRef) (PriceResult, error) {
	var ids []uint
	if err := tx.Unscoped().Model(&Models.TripStruct{}).
		Where("parent_trip_id = ?", parentID).Pluck("id", &ids).Error; err != nil {
		return PriceResult{}, err
	}
	return PriceTrips(tx, ids, extra...)
}

// MonthsOfTrips is the company-months the given rows sit in now. Call it
// before an edit that may move them, and pass the result to PriceTrips after.
func MonthsOfTrips(tx *gorm.DB, ids []uint) ([]MonthRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return monthsOfGroups(tx, `SELECT id FROM trips WHERE id IN ?`, ids)
}

// MonthsOfParent is MonthsOfTrips for a parent trip's containers.
func MonthsOfParent(tx *gorm.DB, parentID uint) ([]MonthRef, error) {
	return monthsOfGroups(tx, `SELECT id FROM trips WHERE parent_trip_id = ?`, parentID)
}

// monthsOfGroups lists the company-months of every row (live or deleted) in
// the logical trips of the seed rows.
func monthsOfGroups(tx *gorm.DB, seedSQL string, args ...any) ([]MonthRef, error) {
	var out []MonthRef
	err := tx.Raw(`
        WITH seed AS (
            SELECT DISTINCT COALESCE(NULLIF(t.parent_trip_id, 0), -t.id) AS g
              FROM trips t WHERE t.id IN (`+seedSQL+`)
        )
        SELECT DISTINCT t.company, left(t.date, 7) || '-01' AS month
          FROM trips t
          JOIN seed s ON s.g = COALESCE(NULLIF(t.parent_trip_id, 0), -t.id)
         WHERE t.date ~ `+validDate+` AND COALESCE(t.company, '') <> ''`, args...).
		Scan(&out).Error
	return out, err
}

// pricedCTEs are the engine's formula: given a CTE named live (id, company,
// terminal, drop_off_point, date, tank_capacity, g, date_ok, old_base,
// old_error), joined finds each row's fee mapping and effective rule and
// computed gives its base revenue, VAT rate, priced km or pricing error. It
// takes one parameter, the unregistered drop-off. priceSeed stores what it
// computes; Quote prices hypothetical drops with the very same text.
const pricedCTEs = `
        joined AS (
            SELECT l.*,
                   fm.id                AS fm_id,
                   fm.distance::numeric AS fm_distance,
                   fm.fee::numeric      AS fm_fee,
                   r.id                 AS rule_id,
                   r.method, r.per_km_rate, r.vat_rate,
                   pb.rate_per_1000l    AS band_rate,
                   ROW_NUMBER() OVER (
                       PARTITION BY l.company, l.g
                       ORDER BY COALESCE(fm.distance, 0) DESC, l.id
                   ) AS rn
              FROM live l
              LEFT JOIN fee_mappings fm
                ON  fm.company        = l.company
                AND fm.terminal       = l.terminal
                AND fm.drop_off_point = l.drop_off_point
                AND fm.deleted_at IS NULL
              LEFT JOIN LATERAL (
                    SELECT pr.* FROM pricing_rules pr
                     WHERE l.date_ok
                       AND pr.company = l.company
                       -- ISO dates compare correctly as text, and a text
                       -- compare cannot fail on a value like 2025-02-30.
                       AND to_char(pr.effective_from, 'YYYY-MM-DD') <= l.date
                     ORDER BY pr.effective_from DESC
                     LIMIT 1
              ) r ON true
              LEFT JOIN pricing_bands pb
                ON  r.method = 'band_per_1000l'
                AND pb.rule_id = r.id
                AND pb.band = fm.fee::int
        ),
        computed AS (
            SELECT j.id, j.rule_id, j.vat_rate, j.old_base, j.old_error,
                   CASE
                     WHEN NOT j.date_ok THEN
                       'invalid trip date "' || COALESCE(j.date, '') || '"'
                     WHEN j.rule_id IS NULL THEN
                       'no pricing rule for company "' || COALESCE(j.company, '') || '" on ' || j.date
                     WHEN j.drop_off_point = ? THEN
                       'unregistered drop-off (' || j.drop_off_point || ')'
                     WHEN j.fm_id IS NULL THEN
                       'no fee mapping for ' || COALESCE(j.company, '') || ' / ' ||
                       COALESCE(j.terminal, '') || ' -> ' || COALESCE(j.drop_off_point, '')
                     WHEN j.method = 'band_per_1000l' AND j.band_rate IS NULL THEN
                       'band ' || COALESCE(j.fm_fee::text, 'NULL') || ' has no rate in pricing rule ' || j.rule_id
                     WHEN j.method = 'per_1000l_fee' AND j.fm_fee IS NULL THEN
                       'fee mapping has no fee'
                   END AS err,
                   CASE j.method
                     WHEN 'per_1000l_fee'  THEN j.tank_capacity * j.fm_fee / 1000
                     WHEN 'band_per_1000l' THEN j.tank_capacity * j.band_rate / 1000
                     WHEN 'per_km' THEN
                       CASE WHEN j.rn = 1 THEN COALESCE(j.fm_distance, 0) * j.per_km_rate ELSE 0 END
                   END AS base,
                   CASE WHEN j.method = 'per_km' THEN
                       CASE WHEN j.rn = 1 THEN COALESCE(j.fm_distance, 0) ELSE 0 END
                   END AS km
              FROM joined j
        )`

// priceSeed prices every live row in the logical trips of the seed rows
// (seedSQL selects trip ids; args are its parameters) under the rule
// effective on each row's date.
func priceSeed(tx *gorm.DB, seedSQL string, args ...any) (PriceResult, error) {
	var res PriceResult
	// Rules are looked up per row with LATERAL: the newest version whose
	// effective_from is on or before the trip's date.
	q := `
        WITH seed AS (
            SELECT DISTINCT COALESCE(NULLIF(t.parent_trip_id, 0), -t.id) AS g
              FROM trips t WHERE t.id IN (` + seedSQL + `)
        ),
        live AS (
            SELECT t.id, t.company, t.terminal, t.drop_off_point, t.date,
                   t.tank_capacity::numeric AS tank_capacity,
                   COALESCE(NULLIF(t.parent_trip_id, 0), -t.id) AS g,
                   COALESCE(t.date ~ ` + validDate + `, false) AS date_ok,
                   t.base_revenue AS old_base, t.pricing_error AS old_error
              FROM trips t
              JOIN seed s ON s.g = COALESCE(NULLIF(t.parent_trip_id, 0), -t.id)
             WHERE t.deleted_at IS NULL
        ),
        ` + pricedCTEs + `,
        upd AS (
            UPDATE trips t SET
                base_revenue    = CASE WHEN c.err IS NULL THEN c.base END,
                revenue_vat     = CASE WHEN c.err IS NULL THEN c.base * c.vat_rate END,
                priced_km       = CASE WHEN c.err IS NULL THEN c.km END,
                pricing_rule_id = c.rule_id,
                pricing_error   = c.err,
                priced_at       = now(),
                revenue         = COALESCE(CASE WHEN c.err IS NULL THEN c.base END, 0)
              FROM computed c
             WHERE t.id = c.id
            RETURNING c.err IS NOT NULL AS failed,
                      (c.old_base IS DISTINCT FROM t.base_revenue
                       OR c.old_error IS DISTINCT FROM t.pricing_error) AS changed
        )
        SELECT COUNT(*) AS priced,
               COUNT(*) FILTER (WHERE changed) AS changed,
               COUNT(*) FILTER (WHERE failed) AS errors
          FROM upd`
	var counts struct{ Priced, Changed, Errors int64 }
	all := append(append([]any{}, args...), UnregisteredDropOff)
	if err := tx.Raw(q, all...).Scan(&counts).Error; err != nil {
		return res, err
	}
	res.Priced, res.Changed, res.Errors = counts.Priced, counts.Changed, counts.Errors
	return res, nil
}

/* ------------------------------------------------------------------------ */
/* Truck-month rental                                                        */
/* ------------------------------------------------------------------------ */

type rentalRule struct {
	ID             uint
	RentalMode     string
	RentalAmount   *float64
	RentalFullDays *int
	VatRate        float64
}

// rentalRuleFor is the rule a company-month's rental is computed under: the
// version effective on the month's first day, or — for a company whose first
// version starts mid-month — the earliest one starting within the month.
// Rental is a monthly contract, so a version that starts mid-month applies to
// rental from the next month; date a rental change to a month's first day.
func rentalRuleFor(tx *gorm.DB, m MonthRef) (*rentalRule, error) {
	var rules []rentalRule
	err := tx.Raw(`
        (SELECT id, rental_mode, rental_amount, rental_full_days, vat_rate
           FROM pricing_rules
          WHERE company = ? AND effective_from <= ?::date
          ORDER BY effective_from DESC LIMIT 1)
        UNION ALL
        (SELECT id, rental_mode, rental_amount, rental_full_days, vat_rate
           FROM pricing_rules
          WHERE company = ? AND effective_from > ?::date
            AND effective_from < (?::date + interval '1 month')
          ORDER BY effective_from ASC LIMIT 1)`,
		m.Company, m.Month, m.Company, m.Month, m.Month).Scan(&rules).Error
	if err != nil || len(rules) == 0 {
		return nil, err
	}
	return &rules[0], nil
}

// IsFrozen reports whether a company-month's rental is frozen.
func IsFrozen(tx *gorm.DB, m MonthRef) (bool, error) {
	var n int64
	err := tx.Raw(`SELECT COUNT(*) FROM truck_month_charges
                    WHERE company = ? AND month = ?::date AND frozen_at IS NOT NULL`,
		m.Company, m.Month).Scan(&n).Error
	return n > 0, err
}

// RecomputeMonths recomputes the truck-month rental of each company-month
// from the live trips in it. A frozen month is skipped (and reported in the
// second list) unless includeFrozen, in which case it is recomputed and
// re-frozen at now(). A company with no rule or no rental has its rows for the
// month removed. Duplicates in months are ignored.
func RecomputeMonths(tx *gorm.DB, months []MonthRef, includeFrozen bool) (recomputed, frozen []MonthRef, err error) {
	for _, m := range dedupe(months) {
		wasFrozen, err := IsFrozen(tx, m)
		if err != nil {
			return recomputed, frozen, err
		}
		if wasFrozen && !includeFrozen {
			frozen = append(frozen, m)
			continue
		}
		if err := recomputeMonth(tx, m, wasFrozen); err != nil {
			return recomputed, frozen, fmt.Errorf("recompute %s %s: %w", m.Company, m.Month, err)
		}
		recomputed = append(recomputed, m)
	}
	return recomputed, frozen, nil
}

func dedupe(months []MonthRef) []MonthRef {
	seen := map[MonthRef]bool{}
	out := make([]MonthRef, 0, len(months))
	for _, m := range months {
		if m.Company == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Month != out[j].Month {
			return out[i].Month < out[j].Month
		}
		return out[i].Company < out[j].Company
	})
	return out
}

// The trips a company-month's rental is earned by: every live row of the
// company dated in the month, priced or not. An unmapped route earns no base
// revenue but still uses the car for the day, as in apex-rust.
const monthTrips = `
        d AS (
            SELECT COALESCE(car_no_plate, '') AS car_no_plate,
                   COALESCE(terminal, '')     AS terminal,
                   date, NULLIF(car_id, 0)    AS car_id
              FROM trips
             WHERE deleted_at IS NULL
               AND company = @company
               AND date ~ ` + validDate + `
               AND date >= @month_start AND date < @month_end
        ),
        per_car AS (
            SELECT car_no_plate, COUNT(DISTINCT date) AS month_days, MAX(car_id) AS car_id
              FROM d GROUP BY car_no_plate
        )`

// TAQA: one rental per car-month, full at rental_full_days days, tapered by
// rental_amount/rental_full_days per day short (floored at 0), then divided
// between terminals by the days each saw (stats_queries.rs get_taqa_stats).
const monthlyTaperInsert = `
        WITH ` + monthTrips + `,
        per_term AS (
            SELECT car_no_plate, terminal, COUNT(DISTINCT date) AS term_days
              FROM d GROUP BY car_no_plate, terminal
        ),
        car_rental AS (
            SELECT car_no_plate, month_days, car_id,
                   CASE
                     WHEN month_days >= @full_days THEN CAST(@amount AS numeric)
                     WHEN month_days > 0 THEN GREATEST(0, CAST(@amount AS numeric)
                          - (@full_days - month_days) * (CAST(@amount AS numeric) / @full_days))
                     ELSE 0
                   END AS rental
              FROM per_car
        ),
        rows AS (
            SELECT pt.car_no_plate, cr.car_id, pt.terminal, pt.term_days, cr.month_days,
                   cr.month_days::numeric * pt.term_days
                     / SUM(pt.term_days) OVER (PARTITION BY pt.car_no_plate) AS chargeable,
                   cr.rental * pt.term_days
                     / SUM(pt.term_days) OVER (PARTITION BY pt.car_no_plate) AS rental
              FROM per_term pt JOIN car_rental cr USING (car_no_plate)
        )`

// Petromin: rental_amount per distinct car-day; a day the car served several
// terminals is split equally between them (stats_queries.rs get_petromin_stats).
const perCarDayInsert = `
        WITH ` + monthTrips + `,
        car_days AS (SELECT DISTINCT car_no_plate, terminal, date FROM d),
        shares AS (
            SELECT car_no_plate, terminal,
                   1.0 / COUNT(*) OVER (PARTITION BY car_no_plate, date) AS share
              FROM car_days
        ),
        rows AS (
            SELECT s.car_no_plate, pc.car_id, s.terminal,
                   COUNT(*)          AS term_days,
                   pc.month_days,
                   SUM(s.share)      AS chargeable,
                   SUM(s.share) * CAST(@amount AS numeric) AS rental
              FROM shares s JOIN per_car pc USING (car_no_plate)
             GROUP BY s.car_no_plate, pc.car_id, s.terminal, pc.month_days
        )`

// Stored at 6 dp: well below a piastre, and it keeps a third of a car-day
// from being stored as 0.33333333333333333333.
const insertRows = `
        INSERT INTO truck_month_charges
            (car_no_plate, car_id, company, terminal, month, days_worked,
             month_days_worked, chargeable_days, rental, rental_vat, rule_id,
             computed_at, frozen_at)
        SELECT car_no_plate, car_id, @company, terminal, CAST(@month_start AS date), term_days,
               month_days, ROUND(chargeable, 6), ROUND(rental, 6),
               ROUND(rental * CAST(@vat AS numeric), 6), @rule_id, now(),
               CASE WHEN @refreeze THEN now() END
          FROM rows`

func recomputeMonth(tx *gorm.DB, m MonthRef, refreeze bool) error {
	if err := tx.Exec(`DELETE FROM truck_month_charges WHERE company = ? AND month = ?::date`,
		m.Company, m.Month).Error; err != nil {
		return err
	}
	rule, err := rentalRuleFor(tx, m)
	if err != nil || rule == nil {
		return err
	}
	start, err := time.Parse("2006-01-02", m.Month)
	if err != nil {
		return err
	}
	params := map[string]any{
		"company":     m.Company,
		"month_start": m.Month,
		"month_end":   start.AddDate(0, 1, 0).Format("2006-01-02"),
		"vat":         rule.VatRate,
		"rule_id":     rule.ID,
		"refreeze":    refreeze,
	}
	if rule.RentalAmount != nil {
		params["amount"] = *rule.RentalAmount
	}
	switch rule.RentalMode {
	case Models.RentalMonthlyTaper:
		if rule.RentalAmount == nil || rule.RentalFullDays == nil || *rule.RentalFullDays <= 0 {
			return fmt.Errorf("rule %d: monthly_taper needs rental_amount and rental_full_days", rule.ID)
		}
		params["full_days"] = *rule.RentalFullDays
		return tx.Exec(monthlyTaperInsert+insertRows, params).Error
	case Models.RentalPerCarDay:
		if rule.RentalAmount == nil {
			return fmt.Errorf("rule %d: per_car_day needs rental_amount", rule.ID)
		}
		return tx.Exec(perCarDayInsert+insertRows, params).Error
	default:
		return nil
	}
}

// Columns lists the stored-pricing columns on trips. Write paths Omit them
// from their own saves so the engine stays their only writer.
var Columns = []string{"revenue", "base_revenue", "revenue_vat", "priced_km",
	"pricing_rule_id", "priced_at", "pricing_error"}

// Hydrate reloads the stored-pricing fields of trips (by ID) in place, for a
// response built from structs the engine has since priced.
func Hydrate(tx *gorm.DB, trips []Models.TripStruct) error {
	if len(trips) == 0 {
		return nil
	}
	ids := make([]uint, len(trips))
	for i := range trips {
		ids[i] = trips[i].ID
	}
	var rows []Models.TripStruct
	if err := tx.Unscoped().Model(&Models.TripStruct{}).
		Select(append([]string{"id"}, Columns...)).
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	byID := make(map[uint]Models.TripStruct, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for i := range trips {
		if r, ok := byID[trips[i].ID]; ok {
			trips[i].Revenue, trips[i].BaseRevenue, trips[i].RevenueVAT = r.Revenue, r.BaseRevenue, r.RevenueVAT
			trips[i].PricedKm, trips[i].PricingRuleID = r.PricedKm, r.PricingRuleID
			trips[i].PricedAt, trips[i].PricingError = r.PricedAt, r.PricingError
		}
	}
	return nil
}

// QuoteResult is one drop-off's quote. Base and VAT are nil, and Error set,
// when the drop could not be priced.
type QuoteResult struct {
	DropOffPoint string   `json:"drop_off_point"`
	Base         *float64 `json:"base_revenue"`
	VAT          *float64 `json:"revenue_vat"`
	Error        *string  `json:"pricing_error"`
}

// Quote is what a trip would earn, priced by the engine's own formula
// (pricedCTEs) without storing anything: one standalone drop of capacity
// litres from terminal to each of company's mapped drop-offs, dated date
// (YYYY-MM-DD). Car rental is not included: it is per truck-month, not per
// trip.
func Quote(db *gorm.DB, company, terminal, date string, capacity float64) ([]QuoteResult, error) {
	var out []QuoteResult
	err := db.Raw(`
        WITH live AS (
            SELECT fm.id, fm.company, fm.terminal, fm.drop_off_point,
                   CAST(? AS text) AS date, CAST(? AS numeric) AS tank_capacity,
                   -fm.id AS g, COALESCE(CAST(? AS text) ~ `+validDate+`, false) AS date_ok,
                   NULL::numeric AS old_base, NULL::text AS old_error
              FROM fee_mappings fm
             WHERE fm.company = ? AND fm.terminal = ? AND fm.deleted_at IS NULL
        ),
        `+pricedCTEs+`
        SELECT l.drop_off_point,
               CASE WHEN c.err IS NULL THEN c.base::float8 END              AS base,
               CASE WHEN c.err IS NULL THEN (c.base * c.vat_rate)::float8 END AS vat,
               c.err AS error
          FROM computed c JOIN live l ON l.id = c.id
         ORDER BY l.drop_off_point`,
		date, capacity, date, company, terminal, UnregisteredDropOff).Scan(&out).Error
	return out, err
}
