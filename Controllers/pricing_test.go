package Controllers

import (
	"Falcon/Constants"
	"Falcon/Models"
	"Falcon/middleware"
	"Falcon/pricing"
	"Falcon/testutil"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type pricingHarness struct {
	t   *testing.T
	db  *gorm.DB
	app *fiber.App
	tok map[int]string // permission -> token
}

func newPricingHarness(t *testing.T) *pricingHarness {
	t.Helper()
	// Keep the side effects of the trip handlers off the network.
	t.Setenv("ETIT_PROXY_ADMIN_URL", "")
	t.Setenv("ETIT_PROXY_ADMIN_TOKEN", "")
	db := testutil.TripSchema(t)
	prevDB, prevSecret := Models.DB, Constants.JWTSecret
	Models.DB, Constants.JWTSecret = db, "pricing-test-secret"
	t.Cleanup(func() { Models.DB, Constants.JWTSecret = prevDB, prevSecret })

	h := &pricingHarness{t: t, db: db, tok: map[int]string{}}
	for _, perm := range []int{1, 3, 4} {
		u := Models.User{Name: fmt.Sprintf("p%d", perm), Email: fmt.Sprintf("p%d@example.invalid", perm), Permission: perm}
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("user: %v", err)
		}
		h.tok[perm] = maintToken(t, u.Id, perm)
	}

	h.app = fiber.New()
	api := h.app.Group("/api")
	api.Get("/pricing/rules", middleware.Verify(3), GetPricingRules)
	api.Post("/pricing/rules", middleware.Verify(4), CreatePricingRule)
	api.Delete("/pricing/rules/:id", middleware.Verify(4), DeletePricingRule)
	api.Get("/pricing/truck-months", middleware.Verify(3), GetTruckMonths)
	api.Post("/pricing/reprice", middleware.Verify(4), RepricePricing)
	trips := api.Group("/trips", middleware.Verify(1))
	th := NewTripHandler(db)
	trips.Put("/:id", th.UpdateTrip)
	trips.Delete("/:id", th.DeleteTrip)
	trips.Post("/multi-container", th.CreateMultiContainerTrip)
	trips.Put("/parent/:parent_id", th.UpdateParentTrip)
	trips.Delete("/parent/:parent_id", th.DeleteParentTrip)
	return h
}

func (h *pricingHarness) do(method, path string, perm int, body any) (int, map[string]any) {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	if perm > 0 {
		req.Header.Set("Cookie", "jwt="+h.tok[perm])
	}
	resp, err := h.app.Test(req, -1)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 500 {
		h.t.Logf("%s %s -> %d %s", method, path, resp.StatusCode, raw)
	}
	return resp.StatusCode, out
}

type pricedRow struct {
	ID           uint
	DropOffPoint string
	Date         string
	Base         *float64
	Km           *float64
	PricingError *string
	Revenue      float64
}

func (h *pricingHarness) rows(where string, args ...any) []pricedRow {
	h.t.Helper()
	var rows []pricedRow
	h.db.Raw(`SELECT id, drop_off_point, date, base_revenue::float8 AS base, priced_km::float8 AS km,
                     pricing_error, revenue::float8 AS revenue
                FROM trips WHERE deleted_at IS NULL AND `+where+` ORDER BY id`, args...).Scan(&rows)
	return rows
}

func (h *pricingHarness) rental(company, month string) (float64, int64) {
	var r struct {
		Rental float64
		N      int64
	}
	h.db.Raw(`SELECT COALESCE(SUM(rental), 0)::float8 AS rental, COUNT(*) AS n FROM truck_month_charges
               WHERE company = ? AND month = ?::date`, company, month).Scan(&r)
	return r.Rental, r.N
}

func f2(p *float64) string {
	if p == nil {
		return "NULL"
	}
	return fmt.Sprintf("%.2f", *p)
}

// TestWritePathsPriceTrips: every trip write path leaves stored pricing and
// the truck-month rental consistent with the trips as they now stand.
func TestWritePathsPriceTrips(t *testing.T) {
	h := newPricingHarness(t)
	h.db.Exec(`INSERT INTO fee_mappings (company, terminal, drop_off_point, distance, fee) VALUES
                 ('TAQA','T','Near',100,0), ('TAQA','T','Far',300,0), ('Watanya','W','B1',100,1)`)

	// Create: a two-container TAQA trip bills the furthest drop once.
	code, body := h.do("POST", "/api/trips/multi-container", 1, map[string]any{
		"parent_trip": map[string]any{"company": "TAQA", "terminal": "T", "date": "2026-10-05",
			"car_no_plate": "C1", "driver_name": "D"},
		"containers": []map[string]any{
			{"drop_off_point": "Near", "receipt_no": "1200001", "tank_capacity": 10000},
			{"drop_off_point": "Far", "receipt_no": "1200002", "tank_capacity": 10000},
		},
	})
	if code != fiber.StatusCreated {
		t.Fatalf("create = %d %v", code, body)
	}
	parentID := uint(body["parent_trip"].(map[string]any)["ID"].(float64))
	containers := body["containers"].([]any)
	if got := containers[1].(map[string]any)["base_revenue"]; got != 15150.0 {
		t.Errorf("response container base_revenue = %v; want 15150 (300 km × 50.5)", got)
	}
	rows := h.rows("parent_trip_id = ?", parentID)
	if f2(rows[0].Base) != "0.00" || f2(rows[1].Base) != "15150.00" || f2(rows[1].Km) != "300.00" || rows[1].Revenue != 15150 {
		t.Errorf("after create: %+v", rows)
	}
	if r, _ := h.rental("TAQA", "2026-10-01"); fmt.Sprintf("%.2f", r) != "1535.71" {
		t.Errorf("October rental after one day = %.2f; want 1535.71", r)
	}

	// Update the parent: move it to September and send both containers Near.
	code, body = h.do("PUT", fmt.Sprintf("/api/trips/parent/%d", parentID), 1, map[string]any{
		"parent_trip":       map[string]any{"date": "2026-09-15"},
		"update_containers": true,
		"containers":        []map[string]any{{"ID": rows[1].ID, "drop_off_point": "Near"}},
	})
	if code != fiber.StatusOK {
		t.Fatalf("update parent = %d %v", code, body)
	}
	rows = h.rows("parent_trip_id = ?", parentID)
	if f2(rows[0].Base) != "5050.00" || f2(rows[1].Base) != "0.00" {
		t.Errorf("after parent update: %+v; want the lower id to carry 100 km × 50.5", rows)
	}
	if _, n := h.rental("TAQA", "2026-10-01"); n != 0 {
		t.Errorf("October still has %d truck-month rows after the trip moved out", n)
	}
	if r, _ := h.rental("TAQA", "2026-09-01"); fmt.Sprintf("%.2f", r) != "1535.71" {
		t.Errorf("September rental = %.2f; want 1535.71", r)
	}

	// Update one container: send it Far again. The revenue in the body is
	// ignored — revenue belongs to the engine.
	code, body = h.do("PUT", fmt.Sprintf("/api/trips/%d", rows[1].ID), 1, map[string]any{
		"drop_off_point": "Far", "revenue": 999999,
	})
	if code != fiber.StatusCreated {
		t.Fatalf("update trip = %d %v", code, body)
	}
	if got := body["data"].(map[string]any)["revenue"]; got != 15150.0 {
		t.Errorf("update response revenue = %v; want 15150", got)
	}
	rows = h.rows("parent_trip_id = ?", parentID)
	if f2(rows[0].Base) != "0.00" || f2(rows[1].Base) != "15150.00" {
		t.Errorf("after trip update: %+v", rows)
	}

	// Delete the billed container: its sibling becomes the furthest drop.
	if code, body = h.do("DELETE", fmt.Sprintf("/api/trips/%d", rows[1].ID), 1, nil); code != fiber.StatusOK {
		t.Fatalf("delete trip = %d %v", code, body)
	}
	rows = h.rows("parent_trip_id = ?", parentID)
	if len(rows) != 1 || f2(rows[0].Base) != "5050.00" {
		t.Errorf("after deleting the far container: %+v", rows)
	}

	// Delete the parent: the car-month loses its only day.
	if code, body = h.do("DELETE", fmt.Sprintf("/api/trips/parent/%d", parentID), 1, nil); code != fiber.StatusOK {
		t.Fatalf("delete parent = %d %v", code, body)
	}
	if _, n := h.rental("TAQA", "2026-09-01"); n != 0 {
		t.Errorf("September still has %d truck-month rows after the trip was deleted", n)
	}

	// The unregistered placeholder is stored unpriced, with a reason.
	code, body = h.do("POST", "/api/trips/multi-container", 1, map[string]any{
		"parent_trip": map[string]any{"company": "Watanya", "terminal": "W", "date": "2026-10-06", "car_no_plate": "C2"},
		"containers": []map[string]any{
			{"drop_off_point": "B1", "receipt_no": "700001", "tank_capacity": 20000},
			{"drop_off_point": pricing.UnregisteredDropOff, "receipt_no": "700002", "tank_capacity": 10000},
		},
	})
	if code != fiber.StatusCreated {
		t.Fatalf("create with unregistered drop-off = %d %v", code, body)
	}
	rows = h.rows("receipt_no IN ('700001', '700002')")
	if f2(rows[0].Base) != "2090.00" || rows[0].PricingError != nil {
		t.Errorf("mapped container: %+v; want 20000 L × 104.5 / 1000 = 2090.00", rows[0])
	}
	if rows[1].Base != nil || rows[1].Revenue != 0 || rows[1].PricingError == nil ||
		!strings.Contains(*rows[1].PricingError, "unregistered") {
		t.Errorf("unregistered container: %+v; want NULL revenue and a pricing_error", rows[1])
	}
}

// TestPricingAPI covers the routes' contracts and permissions.
func TestPricingAPI(t *testing.T) {
	h := newPricingHarness(t)

	if code, _ := h.do("GET", "/api/pricing/rules", 1, nil); code != fiber.StatusForbidden {
		t.Errorf("GET rules at permission 1 = %d; want 403", code)
	}
	code, body := h.do("GET", "/api/pricing/rules", 3, nil)
	if code != 200 {
		t.Fatalf("GET rules = %d %v", code, body)
	}
	companies := body["companies"].([]any)
	if len(companies) != 4 {
		t.Fatalf("companies = %v; want the four seeded", companies)
	}
	for _, c := range companies {
		c := c.(map[string]any)
		rules := c["rules"].([]any)
		first := rules[0].(map[string]any)
		if c["current_rule_id"] != first["id"] || first["effective_from"] != "2000-01-01" {
			t.Errorf("%v: current %v, first %v", c["company"], c["current_rule_id"], first)
		}
		if c["company"] == "Watanya" && len(first["bands"].([]any)) != 15 {
			t.Errorf("Watanya bands = %d; want 15", len(first["bands"].([]any)))
		}
	}

	future := time.Now().In(pricing.Cairo).AddDate(0, 2, 0).Format("2006-01") + "-01"
	newRule := map[string]any{"company": "Petromin", "effective_from": future, "method": "per_km",
		"per_km_rate": 45.0, "vat_rate": 0.14, "rental_mode": "per_car_day", "rental_amount": 2100.0}
	if code, _ := h.do("POST", "/api/pricing/rules", 3, newRule); code != fiber.StatusForbidden {
		t.Errorf("POST rules at permission 3 = %d; want 403", code)
	}
	for name, bad := range map[string]map[string]any{
		"per_km without rate":   {"company": "X", "effective_from": future, "method": "per_km", "vat_rate": 0.14},
		"band without bands":    {"company": "X", "effective_from": future, "method": "band_per_1000l", "vat_rate": 0.14},
		"vat as a percent":      {"company": "X", "effective_from": future, "method": "per_1000l_fee", "vat_rate": 14},
		"taper without days":    {"company": "X", "effective_from": future, "method": "per_1000l_fee", "vat_rate": 0, "rental_mode": "monthly_taper", "rental_amount": 1},
		"bad date":              {"company": "X", "effective_from": "2026-13-01", "method": "per_1000l_fee", "vat_rate": 0},
		"duplicate band number": {"company": "X", "effective_from": future, "method": "band_per_1000l", "vat_rate": 0, "bands": []map[string]any{{"band": 1, "rate_per_1000l": 1}, {"band": 1, "rate_per_1000l": 2}}},
	} {
		if code, body := h.do("POST", "/api/pricing/rules", 4, bad); code != fiber.StatusBadRequest {
			t.Errorf("%s: POST = %d %v; want 400", name, code, body)
		}
	}
	code, body = h.do("POST", "/api/pricing/rules", 4, newRule)
	if code != fiber.StatusCreated {
		t.Fatalf("POST rule = %d %v", code, body)
	}
	created := body["rule"].(map[string]any)
	if created["effective_from"] != future || created["per_km_rate"] != 45.0 {
		t.Errorf("created rule = %v", created)
	}
	if code, _ := h.do("POST", "/api/pricing/rules", 4, newRule); code != fiber.StatusConflict {
		t.Errorf("duplicate version = %d; want 409", code)
	}
	// The future version can be withdrawn; the seed cannot.
	if code, body := h.do("DELETE", fmt.Sprintf("/api/pricing/rules/%v", created["id"]), 4, nil); code != 200 {
		t.Errorf("delete future rule = %d %v", code, body)
	}
	seedID := companies[0].(map[string]any)["current_rule_id"]
	if code, _ := h.do("DELETE", fmt.Sprintf("/api/pricing/rules/%v", seedID), 4, nil); code != fiber.StatusConflict {
		t.Errorf("delete an in-force rule = %d; want 409", code)
	}

	// Reprice and truck-months over the apex-rust fixture.
	if err := h.db.Exec(testutil.ApexRevenueFixture).Error; err != nil {
		t.Fatal(err)
	}
	reprice := map[string]any{"from": "2025-05-01", "to": "2025-06-30"}
	if code, _ := h.do("POST", "/api/pricing/reprice", 3, reprice); code != fiber.StatusForbidden {
		t.Errorf("reprice at permission 3 = %d; want 403", code)
	}
	code, body = h.do("POST", "/api/pricing/reprice", 4, reprice)
	if code != 200 {
		t.Fatalf("reprice = %d %v", code, body)
	}
	after := body["after"].(map[string]any)
	if body["trips_repriced"] != 56.0 || fmt.Sprintf("%.2f", after["rental"]) != "68964.29" ||
		body["pricing_errors"] != 2.0 {
		t.Errorf("reprice = %v", body)
	}
	if b := body["before"].(map[string]any); b["base_revenue"] != 0.0 || b["unpriced"] != 56.0 {
		t.Errorf("before = %v; want nothing priced yet", b)
	}

	code, body = h.do("GET", "/api/pricing/truck-months?month=2025-05", 3, nil)
	if code != 200 || len(body["rows"].([]any)) != 4 {
		t.Fatalf("truck-months = %d %v", code, body)
	}
	var lines []string
	for _, tl := range body["totals"].([]any) {
		tl := tl.(map[string]any)
		lines = append(lines, fmt.Sprintf("%v cars=%v days=%v rental=%.2f", tl["company"], tl["cars"], tl["car_days"], tl["rental"]))
	}
	if got := strings.Join(lines, "; "); got != "Petromin cars=2 days=3 rental=6000.00; TAQA cars=2 days=38 rental=58357.14" {
		t.Errorf("May totals = %s", got)
	}
	if code, _ := h.do("GET", "/api/pricing/truck-months?month=May", 3, nil); code != fiber.StatusBadRequest {
		t.Errorf("bad month = %d; want 400", code)
	}

	// Frozen months need the explicit flag.
	if _, err := pricing.FreezeClosedMonths(h.db, time.Now()); err != nil {
		t.Fatal(err)
	}
	code, body = h.do("POST", "/api/pricing/reprice", 4, reprice)
	if code != fiber.StatusConflict || len(body["frozen_months"].([]any)) != 2 {
		t.Errorf("reprice over frozen months = %d %v; want 409 listing 2025-05 and 2025-06", code, body)
	}
	reprice["include_frozen"] = true
	code, body = h.do("POST", "/api/pricing/reprice", 4, reprice)
	if code != 200 || body["trips_changed"] != 0.0 {
		t.Errorf("reprice include_frozen = %d %v; want 200 and nothing changed", code, body)
	}
}
