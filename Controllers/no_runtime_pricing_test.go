package Controllers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoRuntimePricingFormulaLeft: revenue is priced once, on write, by
// package pricing from the pricing_rules tables. No read path may carry its
// own copy of a rate, a band table, a rental formula or a per-company price
// branch again — that is how five places drifted apart. Comments are skipped;
// the seed (Models/pricing.go) and the engine (pricing/) are the only homes.
func TestNoRuntimePricingFormulaLeft(t *testing.T) {
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`\b(104\.5|122\.1|420\.2|156\.2)\b`),        // Watanya band rates
		regexp.MustCompile(`\b(44\.5|42\.5|50\.5)\b`),                  // per-km rates
		regexp.MustCompile(`\b(43000|1433)\b|\b2000\.0\b|\*\s*2000\b`), // rental
		regexp.MustCompile(`\*\s*1\.14\b|\*\s*0\.14\b`),                // VAT
		regexp.MustCompile(`(?i)avg\((fm\.)?fee\)`),                    // the unknown-company fallback
		regexp.MustCompile(`wtFeeIndexToAmount|WatanyaBandRates|groupPetrominTripsByCapacity|getMaxDistanceFromTripGroup|groupTripsForSingleDate|calculateGroupStats|GetTripStatsByRoute|GetTripStatsByTime`),
		regexp.MustCompile(`(case|==)\s*"(Petrol Arrows|TAQA|Petromin)"`), // per-company price branches
		regexp.MustCompile(`terminal\s*==\s*"(Alex|Suez)"|'Alex'|'Suez'`),
	}
	var checked int
	for _, dir := range []string{".", "../services", "../Apis", "../FiberConfig", "../workers", "../report"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			checked++
			for i, line := range strings.Split(string(raw), "\n") {
				code := strings.TrimSpace(line)
				if strings.HasPrefix(code, "//") {
					continue
				}
				for _, re := range forbidden {
					if re.MatchString(code) {
						t.Errorf("%s:%d: runtime pricing outside package pricing: %s", f, i+1, code)
					}
				}
			}
		}
	}
	if checked < 20 {
		t.Fatalf("checked only %d files; the paths are wrong", checked)
	}
}
