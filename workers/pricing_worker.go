package workers

import (
	"Falcon/Observability"
	"Falcon/pricing"
	"encoding/json"
	"log"
	"time"

	"gorm.io/gorm"
)

// PricingWorker keeps stored trip pricing current:
//
//   - once, on the first boot after stored pricing ships, it backfills every
//     unpriced trip and every truck-month (pricing_jobs marks it done);
//   - every hour it freezes the truck-months of months that have closed (day
//     pricing.FreezeAfterDays+1 of the next month, Cairo);
//   - once a night (after 02:00 Cairo) it recomputes every open truck-month,
//     the safety net under the write paths.
type PricingWorker struct {
	DB          *gorm.DB
	ticker      *time.Ticker
	lastNightly string
	backfilled  bool
}

func NewPricingWorker(db *gorm.DB) *PricingWorker {
	return &PricingWorker{DB: db, ticker: time.NewTicker(time.Hour)}
}

func (w *PricingWorker) Start() {
	log.Println("🚀 Pricing Worker started - backfill once, freeze hourly, recompute nightly")
	go func() {
		w.tick(time.Now())
		for now := range w.ticker.C {
			w.tick(now)
		}
	}()
}

func (w *PricingWorker) Stop() { w.ticker.Stop() }

// backfillOnce runs the backfill unless it is marked done, and reports
// whether it is done now. A failed run is retried on the next tick.
func (w *PricingWorker) backfillOnce() bool {
	done, err := pricing.BackfillDone(w.DB)
	if err != nil {
		Observability.ReportJobFailure("pricing_worker", "backfill_probe", err)
		return false
	}
	if done {
		return true
	}
	started := time.Now()
	log.Println("💰 Stored pricing backfill starting")
	res, err := pricing.Backfill(w.DB, started)
	if err != nil {
		// Not marked: the next tick carries on from where this one stopped.
		log.Printf("❌ Stored pricing backfill failed after %d months: %v", res.Months, err)
		Observability.ReportJobFailure("pricing_worker", "backfill", err)
		return false
	}
	detail, _ := json.Marshal(res)
	log.Printf("✅ Stored pricing backfill: %s in %s", detail, time.Since(started).Round(time.Second))
	if err := pricing.MarkBackfillDone(w.DB, string(detail)); err != nil {
		Observability.ReportJobFailure("pricing_worker", "backfill_mark", err)
	}
	return true
}

// tick runs one hourly pass. Each step is guarded on its own so one failure
// neither kills the loop nor skips the other step.
//
// Nothing is frozen or recomputed until the backfill has finished: freezing a
// month the backfill has not reached yet would lock in a partial rental that
// the backfill, which never touches a frozen month, could then not complete.
func (w *PricingWorker) tick(now time.Time) {
	if !w.backfilled {
		func() {
			defer Observability.Guard("pricing_worker")
			w.backfilled = w.backfillOnce()
		}()
		if !w.backfilled {
			return
		}
	}
	func() {
		defer Observability.Guard("pricing_worker")
		if frozen, err := pricing.FreezeClosedMonths(w.DB, now); err != nil {
			Observability.ReportJobFailure("pricing_worker", "freeze", err)
		} else if len(frozen) > 0 {
			log.Printf("🧊 Froze %d truck-month(s): %v", len(frozen), frozen)
		}
	}()
	cairo := now.In(pricing.Cairo)
	today := cairo.Format("2006-01-02")
	if cairo.Hour() < 2 || w.lastNightly == today {
		return
	}
	func() {
		defer Observability.Guard("pricing_worker")
		months, err := pricing.RecomputeOpenMonths(w.DB, now)
		if err != nil {
			Observability.ReportJobFailure("pricing_worker", "nightly_recompute", err)
			return
		}
		w.lastNightly = today
		log.Printf("💰 Nightly truck-month recompute: %d company-month(s)", len(months))
	}()
}
