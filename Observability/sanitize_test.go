package Observability

import (
	"strings"
	"testing"
)

func TestSanitizeMessageRedactsWhatItMust(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		absent  []string
		present []string
	}{
		{
			name:    "credentials in a URL query",
			in:      `Get "https://fms-gps.etit-eg.com/login?user=admin&pass=hunter2": timeout`,
			absent:  []string{"hunter2"},
			present: []string{"fms-gps.etit-eg.com", "timeout", "login"},
		},
		{
			name:    "token in a query string",
			in:      `GET /api/x?token=abc123def456&page=2 failed`,
			absent:  []string{"abc123def456"},
			present: []string{"page=2", "/api/x"},
		},
		{
			name:    "phone number in free text",
			in:      `WhatsApp send to +201001234567 rejected`,
			absent:  []string{"201001234567"},
			present: []string{"rejected", "WhatsApp"},
		},
		{
			name:    "local mobile",
			in:      `driver contact 01001234567 unreachable`,
			absent:  []string{"01001234567"},
			present: []string{"unreachable"},
		},
		{
			name:    "email address",
			in:      `notify failed for ops.lead@example.com`,
			absent:  []string{"ops.lead@example.com"},
			present: []string{"notify failed"},
		},
		{
			name:    "JSON fields",
			in:      `body {"driver_name":"محمد علي","plate":"6584","lat":"30.1"}`,
			absent:  []string{"محمد علي"},
			present: []string{"6584"},
		},
		{
			name:    "bearer token",
			in:      `auth rejected: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig`,
			absent:  []string{"eyJhbGciOiJIUzI1NiJ9"},
			present: []string{"auth rejected"},
		},
		{
			name:    "cookie header dump",
			in:      `redirect had Cookie: SSOCookie=abcdef; Path=/`,
			absent:  []string{"abcdef"},
			present: []string{"redirect had"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeMessage(tc.in)
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("leaked %q\n  in: %s\n out: %s", a, tc.in, got)
				}
			}
			for _, p := range tc.present {
				if !strings.Contains(got, p) {
					t.Errorf("over-redacted, lost %q\n out: %s", p, got)
				}
			}
		})
	}
}

// The point of sanitizing rather than withholding: ordinary diagnostic text
// must survive intact, or we have just reinvented the empty message.
func TestSanitizeMessageKeepsOrdinaryErrors(t *testing.T) {
	for _, s := range []string{
		`dial tcp 10.0.0.4:5432: connect: connection refused`,
		`context deadline exceeded (Client.Timeout exceeded while awaiting headers)`,
		`ERROR: duplicate key value violates unique constraint "trips_pkey" (SQLSTATE 23505)`,
		`record not found`,
		`invalid character '<' looking for beginning of value`,
	} {
		if got := SanitizeMessage(s); got != s {
			t.Errorf("changed an ordinary error:\n in: %s\nout: %s", s, got)
		}
	}
}

func TestSanitizeMessageTruncatesPayloads(t *testing.T) {
	got := SanitizeMessage(strings.Repeat("x", 5000))
	if len(got) > 2100 {
		t.Errorf("length %d, want capped", len(got))
	}
}
