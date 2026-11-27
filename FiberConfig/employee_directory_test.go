package FiberConfig

import (
	"Falcon/Models"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Every member of staff may list the employees' id, name, type and active
// flag (the garage's attendance list); phone numbers, loans and credit
// balances stay behind permission 3.
func TestEmployeeDirectoryIsOpenAtPermission1WithFourFields(t *testing.T) {
	s := newSurfaces(t)
	SetupEmployeeRoutes(s.app, s.db)
	if err := s.db.AutoMigrate(&Models.Employee{}, &Models.Loan{}, &Models.CreditBalance{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for _, e := range []Models.Employee{
		{Name: "Zed Mechanic", MobileNumber: "01000000001", Type: "garage", IsActive: true},
		{Name: "Amr Office", MobileNumber: "01000000002", Type: "office", IsActive: true},
	} {
		if err := s.db.Create(&e).Error; err != nil {
			t.Fatalf("employee: %v", err)
		}
	}
	gone := Models.Employee{Name: "Gone Worker", MobileNumber: "01000000003", Type: "garage", IsActive: true}
	s.db.Create(&gone)
	s.db.Delete(&gone)

	code, raw := s.do(http.MethodGet, "/api/employee-directory", 1, nil)
	if code != http.StatusOK {
		t.Fatalf("permission 1: %d %s", code, raw)
	}
	if strings.Contains(string(raw), "0100000000") || strings.Contains(string(raw), "credit") || strings.Contains(string(raw), "loans") {
		t.Fatalf("the directory leaks private fields: %s", raw)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("not a list: %s", raw)
	}
	if len(rows) != 2 || rows[0]["name"] != "Amr Office" || rows[1]["type"] != "garage" || rows[1]["is_active"] != true {
		t.Fatalf("rows: %v", rows)
	}
	for _, r := range rows {
		if len(r) != 4 {
			t.Fatalf("want id, name, type, is_active only: %v", r)
		}
	}

	if code, _ := s.do(http.MethodGet, "/api/employee-directory", 0, nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := s.do(http.MethodGet, "/api/employees", 1, nil); code != http.StatusForbidden {
		t.Fatalf("the full list at permission 1: %d", code)
	}
	if code, _ := s.do(http.MethodGet, "/api/employees", 3, nil); code != http.StatusOK {
		t.Fatalf("the full list at permission 3: %d", code)
	}
}
