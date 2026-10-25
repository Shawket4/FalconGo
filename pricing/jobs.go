package pricing

import (
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
)

// Cairo is the business calendar: months open, close and freeze on Cairo dates.
var Cairo = func() *time.Location {
	if loc, err := time.LoadLocation("Africa/Cairo"); err == nil {
		return loc
	}
	return time.FixedZone("EET", 2*60*60)
}()

// FreezeAfterDays is how long a month stays open after it ends: it freezes on
// day FreezeAfterDays+1 of the next month, giving late paperwork two days.
const FreezeAfterDays = 2

/* ------------------------------------------------------------------------ */
/* Totals                                                                    */
/* ------------------------------------------------------------------------ */

// Totals is stored revenue over a date range.
type Totals struct {
	Trips       int64   `json:"trips"`
	Unpriced    int64   `json:"unpriced"`
	BaseRevenue float64 `json:"base_revenue"`
	RevenueVAT  float64 `json:"revenue_vat"`
	Rental      float64 `json:"rental"`
	RentalVAT   float64 `json:"rental_vat"`
	Total       float64 `json:"total"`
}

// RangeTotals sums stored revenue for live trips dated in [from, to] (and the
// rental of every truck-month overlapping it), optionally for one company.
func RangeTotals(tx *gorm.DB, from, to, company string) (Totals, error) {
	var t Totals
	err := tx.Raw(`
        SELECT COUNT(*) AS trips,
               COUNT(*) FILTER (WHERE pricing_error IS NOT NULL OR priced_at IS NULL) AS unpriced,
               COALESCE(SUM(base_revenue), 0)::float8 AS base_revenue,
               COALESCE(SUM(revenue_vat), 0)::float8  AS revenue_vat
          FROM trips
         WHERE deleted_at IS NULL AND date >= ? AND date <= ?
           AND (? = '' OR company = ?)`, from, to, company, company).Scan(&t).Error
	if err != nil {
		return t, err
	}
	var r struct{ Rental, RentalVAT float64 }
	err = tx.Raw(`
        SELECT COALESCE(SUM(rental), 0)::float8 AS rental,
               COALESCE(SUM(rental_vat), 0)::float8 AS rental_vat
          FROM truck_month_charges
         WHERE month >= date_trunc('month', ?::date) AND month <= ?::date
           AND (? = '' OR company = ?)`, from, to, company, company).Scan(&r).Error
	t.Rental, t.RentalVAT = r.Rental, r.RentalVAT
	t.Total = t.BaseRevenue + t.RevenueVAT + t.Rental + t.RentalVAT
	return t, err
}

/* ------------------------------------------------------------------------ */
/* Re-price                                                                  */
/* ------------------------------------------------------------------------ */

// FrozenError is returned by Reprice when the range touches frozen months and
// the caller did not pass includeFrozen.
type FrozenError struct{ Months []string }

func (e *FrozenError) Error() string {
	return fmt.Sprintf("frozen months in range: %v", e.Months)
}

// RepriceResult is the outcome of Reprice.
type RepriceResult struct {
	From             string     `json:"from"`
	To               string     `json:"to"`
	Company          string     `json:"company,omitempty"`
	IncludeFrozen    bool       `json:"include_frozen"`
	TripsRepriced    int64      `json:"trips_repriced"`
	TripsChanged     int64      `json:"trips_changed"`
	PricingErrors    int64      `json:"pricing_errors"`
	MonthsRecomputed []MonthRef `json:"months_recomputed"`
	Before           Totals     `json:"before"`
	After            Totals     `json:"after"`
}

// Reprice re-prices every live trip dated in [from, to] (YYYY-MM-DD,
// inclusive), optionally of one company, under the rules effective on each
// trip's date, and recomputes the truck-months involved. It is the one way an
// already-priced trip moves to a rule created after it was priced.
//
// A month counts as frozen when any of its truck-month rows is frozen — the
// freeze job closes whole months, so this is a property of the calendar month
// rather than of one company. Unless includeFrozen, a range touching one is
// refused with *FrozenError and nothing is written; with it, those months are
// recomputed and re-frozen at now().
func Reprice(tx *gorm.DB, from, to, company string, includeFrozen bool) (RepriceResult, error) {
	res := RepriceResult{From: from, To: to, Company: company, IncludeFrozen: includeFrozen}
	if !includeFrozen {
		var frozen []string
		if err := tx.Raw(`
            SELECT DISTINCT to_char(month, 'YYYY-MM') FROM truck_month_charges
             WHERE frozen_at IS NOT NULL
               AND month >= date_trunc('month', ?::date) AND month <= ?::date
             ORDER BY 1`, from, to).Scan(&frozen).Error; err != nil {
			return res, err
		}
		if len(frozen) > 0 {
			return res, &FrozenError{Months: frozen}
		}
	}

	before, err := RangeTotals(tx, from, to, company)
	if err != nil {
		return res, err
	}
	res.Before = before

	seed := `SELECT id FROM trips WHERE deleted_at IS NULL AND date >= ? AND date <= ? AND (? = '' OR company = ?)`
	args := []any{from, to, company, company}
	priced, err := priceSeed(tx, seed, args...)
	if err != nil {
		return res, err
	}
	months, err := monthsOfGroups(tx, seed, args...)
	if err != nil {
		return res, err
	}
	// Months that hold rental rows but no longer any trip must be emptied too.
	var stale []MonthRef
	if err := tx.Raw(`
        SELECT DISTINCT company, to_char(month, 'YYYY-MM-DD') AS month FROM truck_month_charges
         WHERE month >= date_trunc('month', ?::date) AND month <= ?::date
           AND (? = '' OR company = ?)`, from, to, company, company).Scan(&stale).Error; err != nil {
		return res, err
	}
	recomputed, _, err := RecomputeMonths(tx, append(months, stale...), includeFrozen)
	if err != nil {
		return res, err
	}

	after, err := RangeTotals(tx, from, to, company)
	if err != nil {
		return res, err
	}
	res.TripsRepriced, res.TripsChanged, res.PricingErrors = priced.Priced, priced.Changed, priced.Errors
	res.MonthsRecomputed = recomputed
	if res.MonthsRecomputed == nil {
		res.MonthsRecomputed = []MonthRef{}
	}
	res.After = after
	return res, nil
}

/* ------------------------------------------------------------------------ */
/* Freeze and nightly recompute                                              */
/* ------------------------------------------------------------------------ */

// closedBefore is the first month still open on Cairo date today: every month
// before it has ended at least FreezeAfterDays ago.
func closedBefore(today time.Time) string {
	d := today.In(Cairo).AddDate(0, 0, -FreezeAfterDays)
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, Cairo).Format("2006-01-02")
}

// FreezeClosedMonths recomputes, one last time, every unfrozen company-month
// that has closed as of now, then freezes it. Returns the months frozen.
func FreezeClosedMonths(db *gorm.DB, now time.Time) ([]MonthRef, error) {
	cutoff := closedBefore(now)
	var months []MonthRef
	if err := db.Raw(`
        SELECT DISTINCT company, to_char(month, 'YYYY-MM-DD') AS month
          FROM truck_month_charges
         WHERE frozen_at IS NULL AND month < ?::date`, cutoff).Scan(&months).Error; err != nil {
		return nil, err
	}
	var frozen []MonthRef
	for _, m := range dedupe(months) {
		err := db.Transaction(func(tx *gorm.DB) error {
			if _, _, err := RecomputeMonths(tx, []MonthRef{m}, false); err != nil {
				return err
			}
			return tx.Exec(`UPDATE truck_month_charges SET frozen_at = now()
                             WHERE company = ? AND month = ?::date AND frozen_at IS NULL`,
				m.Company, m.Month).Error
		})
		if err != nil {
			return frozen, err
		}
		frozen = append(frozen, m)
	}
	return frozen, nil
}

// RecomputeOpenMonths recomputes every company-month that is not frozen and
// is either still open as of now or already has unfrozen rows. This is the
// nightly safety net under the write paths: anything that changed trips
// without going through the engine (a manual SQL fix, a fee-mapping rename)
// is folded back into the rental by morning.
func RecomputeOpenMonths(db *gorm.DB, now time.Time) ([]MonthRef, error) {
	cutoff := closedBefore(now)
	var months []MonthRef
	if err := db.Raw(`
        SELECT DISTINCT company, to_char(month, 'YYYY-MM-DD') AS month
          FROM truck_month_charges WHERE frozen_at IS NULL
        UNION
        SELECT DISTINCT t.company, left(t.date, 7) || '-01'
          FROM trips t
          JOIN pricing_rules r ON r.company = t.company AND r.rental_mode <> 'none'
         WHERE t.deleted_at IS NULL AND t.date ~ `+validDate+` AND t.date >= ?`, cutoff).
		Scan(&months).Error; err != nil {
		return nil, err
	}
	var done []MonthRef
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		done, _, err = RecomputeMonths(tx, months, false)
		return err
	})
	return done, err
}

/* ------------------------------------------------------------------------ */
/* Backfill                                                                  */
/* ------------------------------------------------------------------------ */

// BackfillJob is the pricing_jobs marker of the one-shot backfill.
const BackfillJob = "stored_pricing_backfill_v1"

// BackfillResult is what a backfill run did.
type BackfillResult struct {
	Months      int              `json:"months"`
	Trips       int64            `json:"trips"`
	Errors      int64            `json:"errors"`
	TruckMonths int              `json:"truck_months"`
	Frozen      int              `json:"frozen"`
	ErrorKinds  map[string]int64 `json:"error_kinds"`
}

// Backfill prices every live trip that has never been priced (priced_at IS
// NULL), one calendar month per transaction, recomputes the truck-months of
// every month it touched, then freezes the months that have closed. A trip
// already priced is left exactly as it is, so the backfill can be re-run at
// any time — after an interruption it carries on where it stopped — and a
// re-run only fills in what is missing. Rows whose date is not YYYY-MM-DD are
// priced in a last pass and all come out with a pricing_error.
func Backfill(db *gorm.DB, now time.Time) (BackfillResult, error) {
	res := BackfillResult{ErrorKinds: map[string]int64{}}
	var months []string
	if err := db.Raw(`
        SELECT DISTINCT left(date, 7) FROM trips
         WHERE deleted_at IS NULL AND priced_at IS NULL AND date ~ ` + validDate + `
         ORDER BY 1`).Scan(&months).Error; err != nil {
		return res, err
	}
	for _, ym := range months {
		start, err := time.Parse("2006-01", ym)
		if err != nil {
			continue
		}
		from, to := start.Format("2006-01-02"), start.AddDate(0, 1, -1).Format("2006-01-02")
		err = db.Transaction(func(tx *gorm.DB) error {
			seed := `SELECT id FROM trips WHERE deleted_at IS NULL AND priced_at IS NULL AND date >= ? AND date <= ?`
			pr, err := priceSeed(tx, seed, from, to)
			if err != nil {
				return err
			}
			var companies []string
			if err := tx.Raw(`SELECT DISTINCT company FROM trips
                               WHERE deleted_at IS NULL AND date >= ? AND date <= ?
                                 AND COALESCE(company, '') <> ''`, from, to).Scan(&companies).Error; err != nil {
				return err
			}
			refs := make([]MonthRef, 0, len(companies))
			for _, c := range companies {
				refs = append(refs, MonthRef{Company: c, Month: from})
			}
			done, _, err := RecomputeMonths(tx, refs, false)
			if err != nil {
				return err
			}
			res.Trips += pr.Priced
			res.Errors += pr.Errors
			res.TruckMonths += len(done)
			log.Printf("💰 pricing backfill %s: %d trips priced, %d unpriced, %d company-months",
				ym, pr.Priced, pr.Errors, len(done))
			return nil
		})
		if err != nil {
			return res, fmt.Errorf("backfill %s: %w", ym, err)
		}
		res.Months++
	}

	// Malformed dates: nothing to group by month, and nothing to bill.
	if err := db.Transaction(func(tx *gorm.DB) error {
		pr, err := priceSeed(tx, `SELECT id FROM trips WHERE deleted_at IS NULL AND priced_at IS NULL
                                   AND NOT COALESCE(date ~ `+validDate+`, false)`)
		res.Trips += pr.Priced
		res.Errors += pr.Errors
		return err
	}); err != nil {
		return res, err
	}

	frozen, err := FreezeClosedMonths(db, now)
	res.Frozen = len(frozen)
	if err != nil {
		return res, err
	}

	var kinds []struct {
		Kind string
		N    int64
	}
	db.Raw(`SELECT regexp_replace(pricing_error, '"[^"]*"|[0-9][0-9.]*', '…', 'g') AS kind, COUNT(*) AS n
              FROM trips WHERE deleted_at IS NULL AND pricing_error IS NOT NULL
             GROUP BY 1 ORDER BY 2 DESC LIMIT 50`).Scan(&kinds)
	for _, k := range kinds {
		res.ErrorKinds[k.Kind] = k.N
	}
	return res, nil
}

// BackfillDone reports whether the one-shot backfill has finished here.
func BackfillDone(db *gorm.DB) (bool, error) {
	var n int64
	err := db.Raw(`SELECT COUNT(*) FROM pricing_jobs WHERE name = ?`, BackfillJob).Scan(&n).Error
	return n > 0, err
}

// MarkBackfillDone records the backfill as finished.
func MarkBackfillDone(db *gorm.DB, detail string) error {
	return db.Exec(`INSERT INTO pricing_jobs (name, detail) VALUES (?, ?)
                    ON CONFLICT (name) DO UPDATE SET finished_at = now(), detail = EXCLUDED.detail`,
		BackfillJob, detail).Error
}
