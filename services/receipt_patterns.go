package services

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"Falcon/Models"

	"gorm.io/gorm"
)

// Receipt-number serial validation.
//
// Each terminal issues receipts from known serial ranges, per company or for
// every company at once (terminal_receipt_patterns, company NULL). A trip's
// receipt_no is checked against the pattern for its (terminal_id, company) --
// the company-specific pattern wins over the NULL-company one -- and the
// result is surfaced as the computed, non-stored receipt_valid field:
// nil when there is no active pattern to check against, true/false otherwise.
//
// Patterns are loaded once and compiled once; the cache refreshes on a short
// TTL and is invalidated explicitly by the pattern CRUD endpoints.

const receiptPatternCacheTTL = time.Minute

type receiptPatternCache struct {
	mu       sync.RWMutex
	loadedAt time.Time
	// Key is "terminalID\x00company"; the all-companies pattern is keyed with
	// an empty company.
	compiled map[string]*regexp.Regexp
}

var patternCache receiptPatternCache

func receiptPatternKey(terminalID uint, company string) string {
	return fmt.Sprintf("%d\x00%s", terminalID, company)
}

// InvalidateReceiptPatternCache forces the next lookup to reload from the
// database. Called by the receipt-pattern CRUD endpoints.
func InvalidateReceiptPatternCache() {
	patternCache.mu.Lock()
	patternCache.compiled = nil
	patternCache.loadedAt = time.Time{}
	patternCache.mu.Unlock()
}

// receiptPatterns returns the compiled pattern map, reloading it when stale.
func receiptPatterns(db *gorm.DB) map[string]*regexp.Regexp {
	patternCache.mu.RLock()
	if patternCache.compiled != nil && time.Since(patternCache.loadedAt) < receiptPatternCacheTTL {
		compiled := patternCache.compiled
		patternCache.mu.RUnlock()
		return compiled
	}
	patternCache.mu.RUnlock()

	patternCache.mu.Lock()
	defer patternCache.mu.Unlock()
	if patternCache.compiled != nil && time.Since(patternCache.loadedAt) < receiptPatternCacheTTL {
		return patternCache.compiled
	}

	var rows []Models.TerminalReceiptPattern
	if err := db.Where("active = ?", true).Find(&rows).Error; err != nil {
		log.Printf("⚠️ failed to load terminal receipt patterns: %v", err)
		// Keep serving the stale set (or nothing) rather than failing requests.
		if patternCache.compiled == nil {
			patternCache.compiled = map[string]*regexp.Regexp{}
		}
		return patternCache.compiled
	}

	compiled := make(map[string]*regexp.Regexp, len(rows))
	for _, row := range rows {
		re, err := regexp.Compile(row.Pattern)
		if err != nil {
			log.Printf("⚠️ invalid receipt pattern %q for terminal %d: %v", row.Pattern, row.TerminalID, err)
			continue
		}
		company := ""
		if row.Company != nil {
			company = *row.Company
		}
		compiled[receiptPatternKey(row.TerminalID, company)] = re
	}
	patternCache.compiled = compiled
	patternCache.loadedAt = time.Now()
	return compiled
}

// ReceiptValidity checks a receipt number against the active pattern for
// (terminal, company). Returns nil when the trip has no terminal_id or no
// active pattern applies; otherwise whether the (trimmed) number matches.
func ReceiptValidity(db *gorm.DB, terminalID *uint, company, receiptNo string) *bool {
	if terminalID == nil {
		return nil
	}
	patterns := receiptPatterns(db)

	re, ok := patterns[receiptPatternKey(*terminalID, company)]
	if !ok {
		re, ok = patterns[receiptPatternKey(*terminalID, "")]
	}
	if !ok {
		return nil
	}
	valid := re.MatchString(strings.TrimSpace(receiptNo))
	return &valid
}

// EnrichReceiptValidity fills the computed ReceiptValid field on a batch of
// trips, the same way the fee-mapping enrichment fills Distance/Fee.
func EnrichReceiptValidity(db *gorm.DB, trips []Models.TripStruct) {
	if len(trips) == 0 {
		return
	}
	for i := range trips {
		trips[i].ReceiptValid = ReceiptValidity(db, trips[i].TerminalID, trips[i].Company, trips[i].ReceiptNo)
	}
}
