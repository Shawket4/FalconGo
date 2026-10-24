package Controllers

import "testing"

func TestParseAiAuditLimitDefaultsAndClamps(t *testing.T) {
	for raw, want := range map[string]int{"": 100, " ": 100, "1": 1, "250": 250, "500": 500, "501": 500, "100000": 500} {
		got, err := parseAiAuditLimit(raw)
		if err != nil || got != want {
			t.Errorf("parseAiAuditLimit(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "-3", "ten", "1.5"} {
		if _, err := parseAiAuditLimit(raw); err == nil {
			t.Errorf("parseAiAuditLimit(%q) accepted it", raw)
		}
	}
}
