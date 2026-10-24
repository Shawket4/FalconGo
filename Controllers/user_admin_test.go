package Controllers

import (
	"encoding/json"
	"testing"
)

func TestParsePermissionAcceptsStringOrNumberOnTheLadder(t *testing.T) {
	for raw, want := range map[string]int{`4`: 4, `"4"`: 4, `"1"`: 1, ` 3 `: 3} {
		got, err := parsePermission(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("parsePermission(%s) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{`0`, `5`, `"admin"`, `""`, `null`} {
		if _, err := parsePermission(json.RawMessage(raw)); err == nil {
			t.Errorf("parsePermission(%s) accepted an off-ladder value", raw)
		}
	}
}
