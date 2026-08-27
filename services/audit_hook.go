package services

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// NotifyTripAudit tells the etit-proxy to re-audit the given trip dates
// (YYYY-MM-DD). For every parseable date the day before and after are also
// sent, since a trip dated D can physically span D-1..D+1. The proxy
// debounces and coalesces server-side, so this is safe to call on every
// trip mutation. Fire-and-forget: it never blocks or fails the caller, and
// it is a no-op when ETIT_PROXY_ADMIN_URL / ETIT_PROXY_ADMIN_TOKEN are unset.
func NotifyTripAudit(dates ...string) {
	adminURL := os.Getenv("ETIT_PROXY_ADMIN_URL")
	adminToken := os.Getenv("ETIT_PROXY_ADMIN_TOKEN")
	if adminURL == "" || adminToken == "" {
		return
	}

	// Deduplicate, drop empties, and expand each parseable date by +/- 1 day.
	seen := make(map[string]bool)
	expanded := make([]string, 0, len(dates)*3)
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			expanded = append(expanded, d)
		}
	}
	for _, d := range dates {
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

	go func() {
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
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("Warning: trip audit hook: rescan returned status %d", resp.StatusCode)
		}
	}()
}
