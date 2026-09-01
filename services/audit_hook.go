package services

import (
	"Falcon/Observability"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"gorm.io/gorm"
)

// NotifyTripAudit tells the etit-proxy to re-audit the given trip dates
// (YYYY-MM-DD). For every parseable date the day before and after are also
// sent, since a trip dated D can physically span D-1..D+1. The proxy
// debounces and coalesces server-side, so this is safe to call on every
// trip mutation. Fire-and-forget: it never blocks or fails the caller, and
// it is a no-op when ETIT_PROXY_ADMIN_URL / ETIT_PROXY_ADMIN_TOKEN are unset.
// auditHookSlots caps concurrent notifier goroutines. When the proxy is
// down every call burns its full timeout; without a cap a busy import
// session could pile up hundreds of stuck goroutines. Excess notifications
// are DROPPED (with a log line) — safe, because the proxy's nightly
// activity sweep re-derives anything the fast path missed.
var auditHookSlots = make(chan struct{}, 8)

func NotifyTripAudit(dates ...string) {
	// Copy before leaving the caller's stack; the caller's slice must not
	// be retained by the goroutine.
	datesCopy := append([]string(nil), dates...)

	select {
	case auditHookSlots <- struct{}{}:
	default:
		log.Printf("Warning: trip audit hook: notifier saturated, dropping (nightly sweep will cover)")
		return
	}

	// EVERYTHING — env reads, parsing, HTTP — runs off the request path,
	// and the recover guard means a bug here can never take down the
	// process (an unrecovered goroutine panic kills all of Falcon).
	go func() {
		defer func() {
			<-auditHookSlots
			if r := recover(); r != nil {
				log.Printf("Warning: trip audit hook: recovered panic: %v", r)
			}
		}()

		adminURL := os.Getenv("ETIT_PROXY_ADMIN_URL")
		adminToken := os.Getenv("ETIT_PROXY_ADMIN_TOKEN")
		if adminURL == "" || adminToken == "" {
			return
		}

		// Deduplicate, drop empties, expand each parseable date by +/- 1 day.
		seen := make(map[string]bool)
		expanded := make([]string, 0, len(datesCopy)*3)
		add := func(d string) {
			if d != "" && !seen[d] {
				seen[d] = true
				expanded = append(expanded, d)
			}
		}
		for _, d := range datesCopy {
			if d == "" {
				continue
			}
			add(d)
			if t, err := time.Parse("2006-01-02", d); err == nil {
				add(t.AddDate(0, 0, -1).Format("2006-01-02"))
				add(t.AddDate(0, 0, 1).Format("2006-01-02"))
			}
		}
		if len(expanded) == 0 {
			return
		}

		body, err := json.Marshal(map[string][]string{"dates": expanded})
		if err != nil {
			log.Printf("Warning: trip audit hook: failed to marshal dates: %v", err)
			return
		}

		req, err := http.NewRequest(http.MethodPost, adminURL+"/admin/trip-audit/rescan", bytes.NewReader(body))
		if err != nil {
			log.Printf("Warning: trip audit hook: failed to build request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Token", adminToken)

		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Warning: trip audit hook: rescan request failed: %v", err)
			Observability.ReportJobFailure("trip_audit_hook", "rescan_request", err)
			return
		}
		defer resp.Body.Close()
		// Drain so the keep-alive connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode >= 300 {
			log.Printf("Warning: trip audit hook: rescan returned status %d", resp.StatusCode)
		}
	}()
}

// locationRescanWindowDays bounds how far back a pin change re-audits.
// Aligned with the proxy's own activity-sweep horizon; older history can be
// re-audited with an explicit scan when needed.
const locationRescanWindowDays = 45

// NotifyLocationRescan re-audits every recent trip day that a location's
// geofence touches, after its pin or radius changed. kind is "terminal"
// (uses terminalID) or "dropoff" (uses dropoffName). Same guarantees as
// NotifyTripAudit: everything off the request path, panic-contained,
// never blocks the caller.
func NotifyLocationRescan(db *gorm.DB, kind string, terminalID uint, dropoffName string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Warning: location rescan hook: recovered panic: %v", r)
			}
		}()

		floor := time.Now().AddDate(0, 0, -locationRescanWindowDays).Format("2006-01-02")
		var dates []string
		var err error
		switch kind {
		case "terminal":
			err = db.Raw(
				`SELECT DISTINCT date FROM parent_trips
				  WHERE deleted_at IS NULL AND terminal_id = ? AND date >= ?`,
				terminalID, floor,
			).Scan(&dates).Error
		case "dropoff":
			err = db.Raw(
				`SELECT DISTINCT pt.date FROM parent_trips pt
				   JOIN trips t ON t.parent_trip_id = pt.id AND t.deleted_at IS NULL
				  WHERE pt.deleted_at IS NULL AND t.drop_off_point = ? AND pt.date >= ?`,
				dropoffName, floor,
			).Scan(&dates).Error
		default:
			return
		}
		if err != nil {
			log.Printf("Warning: location rescan hook: date query failed: %v", err)
			Observability.ReportJobFailure("location_rescan_hook", "date_query", err)
			return
		}
		if len(dates) == 0 {
			return
		}
		log.Printf("Location pin changed (%s): re-auditing %d trip day(s)", kind, len(dates))
		NotifyTripAudit(dates...)
	}()
}
