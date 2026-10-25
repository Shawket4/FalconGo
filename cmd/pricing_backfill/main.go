// Command pricing_backfill prices every trip that has never been priced and
// computes every truck-month, the same job the server runs once on its first
// boot with stored pricing (workers.PricingWorker).
//
//	go run ./cmd/pricing_backfill            # no-op once the marker is set
//	go run ./cmd/pricing_backfill -force     # run anyway (only fills gaps)
//
// It reads the server's .env. It is safe to re-run: a trip already priced is
// never touched (use POST /api/pricing/reprice to move one), and a frozen
// truck-month is never recomputed.
package main

import (
	"Falcon/Models"
	"Falcon/pricing"
	"encoding/json"
	"flag"
	"log"
	"time"
)

func main() {
	force := flag.Bool("force", false, "run even if the backfill is already marked done")
	flag.Parse()

	// Connect runs the migrations, so the pricing tables and seed exist.
	Models.Connect()

	if done, err := pricing.BackfillDone(Models.DB); err != nil {
		log.Fatalf("probe: %v", err)
	} else if done && !*force {
		log.Println("backfill already done (pass -force to fill any gaps)")
		return
	}
	started := time.Now()
	res, err := pricing.Backfill(Models.DB, started)
	detail, _ := json.MarshalIndent(res, "", "  ")
	if err != nil {
		log.Fatalf("backfill failed: %v\n%s", err, detail)
	}
	if err := pricing.MarkBackfillDone(Models.DB, string(detail)); err != nil {
		log.Fatalf("mark: %v", err)
	}
	log.Printf("done in %s:\n%s", time.Since(started).Round(time.Second), detail)
}
