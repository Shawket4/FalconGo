package FiberConfig

import (
	"Falcon/Constants"
	"Falcon/Controllers"
	"Falcon/Models"
	"Falcon/middleware"
	"Falcon/pricing"
	"Falcon/testutil"
	"Falcon/utils"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// These tests run the real route table (SetupRoutes behind the global
// middleware) over apex-rust's revenue fixture, priced by the stage-A
// backfill: the statistics must report exactly what was stored, and no money
// may reach a caller below permission 3 on any surface.

type surfaces struct {
	t   *testing.T
	db  *gorm.DB
	app *fiber.App
	tok map[int]string
}

func newSurfaces(t *testing.T) *surfaces {
	t.Helper()
	t.Setenv("ETIT_PROXY_ADMIN_URL", "")
	t.Setenv("ETIT_PROXY_ADMIN_TOKEN", "")
	db := testutil.TripSchema(t)
	// What the trip list and the receipt and driver handlers also read.
	if err := db.AutoMigrate(&Models.ReceiptBatch{}, &Models.Receipt{}, &Models.Driver{},
		&Models.DriverSession{}, &Models.Car{}, &Models.LandMark{}, &Models.DropOffPoint{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	prevDB, prevSecret := Models.DB, Constants.JWTSecret
	Models.DB, Constants.JWTSecret = db, "money-surfaces-secret"
	t.Cleanup(func() { Models.DB, Constants.JWTSecret = prevDB, prevSecret })

	s := &surfaces{t: t, db: db, tok: map[int]string{}}
	for _, perm := range []int{1, 2, 3, 4} {
		u := Models.User{Name: fmt.Sprintf("p%d", perm), Email: fmt.Sprintf("p%d@example.invalid", perm), Permission: perm}
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("user: %v", err)
		}
		tok, err := utils.GenerateAdminUserToken(u)
		if err != nil {
			t.Fatal(err)
		}
		s.tok[perm] = tok
	}
	if err := db.Exec(testutil.ApexRevenueFixture).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Drivers for the analytics, and a terminal for the trip details.
	db.Exec(`UPDATE trips SET driver_name = 'Driver ' || car_no_plate`)
	if err := db.Create(&Models.Terminal{Name: "PA-T1"}).Error; err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if _, err := pricing.Backfill(db, time.Date(2026, 10, 25, 12, 0, 0, 0, pricing.Cairo)); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	s.app = fiber.New()
	useGlobalMiddleware(s.app)
	SetupRoutes(s.app, db)
	return s
}

func (s *surfaces) do(method, path string, perm int, body any) (int, []byte) {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	if perm > 0 {
		req.Header.Set("Cookie", "jwt="+s.tok[perm])
	}
	resp, err := s.app.Test(req, -1)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (s *surfaces) json(method, path string, perm int, body any) (int, map[string]any) {
	s.t.Helper()
	code, raw := s.do(method, path, perm, body)
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		s.t.Fatalf("%s %s -> %d: not JSON: %.300s", method, path, code, raw)
	}
	return code, out
}

// moneyFields lists every money key in v with its value, by path.
func moneyFields(v any, path string, out map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			p := path + "." + k
			if middleware.IsMoneyKey(k) {
				out[p] = val
			}
			moneyFields(val, p, out)
		}
	case []any:
		for i, val := range x {
			moneyFields(val, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

func isZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case float64:
		return x == 0
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return err != nil || f == 0
	case map[string]any:
		for _, e := range x {
			if !isZero(e) {
				return false
			}
		}
		return true
	case []any:
		for _, e := range x {
			if !isZero(e) {
				return false
			}
		}
		return true
	}
	return true
}

// nonZeroMoney is every money field in body that carries a value.
func nonZeroMoney(body map[string]any) []string {
	fields := map[string]any{}
	moneyFields(body, "", fields)
	var out []string
	for p, v := range fields {
		if !isZero(v) {
			out = append(out, fmt.Sprintf("%s=%v", p, v))
		}
	}
	sort.Strings(out)
	return out
}

func (s *surfaces) id(where string, args ...any) uint {
	var id uint
	s.db.Raw(`SELECT id FROM trips WHERE `+where+` ORDER BY id LIMIT 1`, args...).Scan(&id)
	return id
}

// TestMoneyIsMaskedBelowPermission3 calls every JSON surface that carries
// revenue, fees, rental or VAT at permissions 1, 2 and 3. Below 3 every money
// field must be zero; at 3 the same call must carry real money, which keeps
// this test from passing on an empty answer.
func TestMoneyIsMaskedBelowPermission3(t *testing.T) {
	s := newSurfaces(t)
	pa := s.id(`receipt_no = 'PA1'`)
	var mappingID uint
	s.db.Raw(`SELECT id FROM fee_mappings WHERE drop_off_point = 'PA-Near'`).Scan(&mappingID)

	type call struct {
		name, method, path string
		body               func(perm int) any
	}
	get := func(name, path string) call { return call{name: name, method: "GET", path: path} }
	calls := []call{
		get("trips list", "/api/trips?limit=100"),
		get("trips by date", "/api/trips/date?start_date=2025-05-01&end_date=2025-06-30&limit=100"),
		get("trips by company", "/api/trips/company/"+url.PathEscape("Petrol Arrows")+"?limit=100"),
		get("trip", fmt.Sprintf("/api/trips/%d", pa)),
		get("trip details", fmt.Sprintf("/api/trips/%d/details", pa)),
		get("parent containers", "/api/trips/parent/9001/containers"),
		get("statistics", "/api/trips/statistics?start_date=2025-05-01&end_date=2025-06-30"),
		get("watanya driver analytics", "/api/trips/watanya/driver-analytics?start_date=2025-05-01&end_date=2025-05-31"),
		get("fee mappings", "/api/mappings/"),
		get("fee mapping", fmt.Sprintf("/api/mappings/%d", mappingID)),
		get("drop-off picker", "/api/mappings/dropoffs/"+url.PathEscape("Petrol Arrows")+"/PA-T1?tank_capacity=40000"),
		get("fee lookup", "/api/mappings/fee?company="+url.QueryEscape("Petrol Arrows")+"&terminal=PA-T1&drop_off_point=PA-Near"),
		get("receipt pile drop-off", "/api/receipt-piles/drop-off?start_date=2025-05-01&end_date=2025-05-31&drop_off_point=WA-B1"),
		get("widget", "/api/stats/widget-data"),
		get("receipt steps of a trip", fmt.Sprintf("/api/receipts/trip/%d", pa)),
		{name: "create multi-container", method: "POST", path: "/api/trips/multi-container", body: func(perm int) any {
			return map[string]any{
				"parent_trip": map[string]any{"company": "TAQA", "terminal": "TQ-T1", "date": "2026-10-05",
					"car_no_plate": fmt.Sprintf("NEW-%d", perm), "driver_name": "D"},
				"containers": []map[string]any{
					{"drop_off_point": "TQ-Site", "receipt_no": fmt.Sprintf("88%d001", perm), "tank_capacity": 10000},
				},
			}
		}},
	}
	for _, c := range calls {
		for _, perm := range []int{1, 2, 3} {
			var body any
			if c.body != nil {
				body = c.body(perm)
			}
			code, resp := s.json(c.method, c.path, perm, body)
			if code >= 300 {
				t.Errorf("%s at permission %d = %d %v", c.name, perm, code, resp)
				continue
			}
			got := nonZeroMoney(resp)
			if perm < 3 && len(got) > 0 {
				t.Errorf("%s at permission %d leaks money: %v", c.name, perm, got)
			}
			if perm == 3 && len(got) == 0 {
				t.Errorf("%s at permission 3 carries no money: the check above proves nothing", c.name)
			}
		}
	}

	// The write paths answer with the trips they priced.
	for _, perm := range []int{1, 3} {
		code, resp := s.json("POST", "/api/trips/multi-container", perm, map[string]any{
			"parent_trip": map[string]any{"company": "TAQA", "terminal": "TQ-T1", "date": "2026-10-06",
				"car_no_plate": fmt.Sprintf("UPD-%d", perm), "driver_name": "D"},
			"containers": []map[string]any{
				{"drop_off_point": "TQ-Site", "receipt_no": fmt.Sprintf("77%d001", perm), "tank_capacity": 10000},
			},
		})
		if code != fiber.StatusCreated {
			t.Fatalf("create = %d %v", code, resp)
		}
		parentID := uint(resp["parent_trip"].(map[string]any)["ID"].(float64))
		code, resp = s.json("PUT", fmt.Sprintf("/api/trips/parent/%d", parentID), perm, map[string]any{
			"parent_trip": map[string]any{"date": "2026-10-07"},
		})
		if code != fiber.StatusOK {
			t.Fatalf("update parent = %d %v", code, resp)
		}
		checkMasked(t, "update parent", perm, resp)
		child := s.id(`parent_trip_id = ?`, parentID)
		code, resp = s.json("PUT", fmt.Sprintf("/api/trips/%d", child), perm, map[string]any{"mileage": 12})
		if code >= 300 {
			t.Fatalf("update trip = %d %v", code, resp)
		}
		checkMasked(t, "update trip", perm, resp)
	}

	// The widget needs a login now.
	if code, _ := s.do("GET", "/api/stats/widget-data", 0, nil); code != fiber.StatusUnauthorized {
		t.Errorf("widget without a login = %d; want 401", code)
	}
	// The Watanya report is permission 3.
	if code, _ := s.do("POST", "/api/trips/watanya/export_report", 1,
		map[string]string{"start_date": "2025-05-01", "end_date": "2025-05-31"}); code != fiber.StatusForbidden {
		t.Errorf("Watanya report at permission 1 = %d; want 403", code)
	}
}

func checkMasked(t *testing.T, name string, perm int, resp map[string]any) {
	t.Helper()
	got := nonZeroMoney(resp)
	if perm < 3 && len(got) > 0 {
		t.Errorf("%s at permission %d leaks money: %v", name, perm, got)
	}
	if perm >= 3 && len(got) == 0 {
		t.Errorf("%s at permission %d carries no money", name, perm)
	}
}

// The trips export leaves the fee and revenue columns out below permission 3,
// and at 3 its revenue is the stored base revenue.
func TestTripsExportRevenueColumns(t *testing.T) {
	s := newSurfaces(t)
	cells := func(perm int) string {
		code, raw := s.do("POST", "/api/trips/export?company=Watanya&start_date=2025-05-01&end_date=2025-05-31", perm, map[string]string{})
		if code != http.StatusOK {
			t.Fatalf("export at %d = %d %.200s", perm, code, raw)
		}
		xl, err := excelize.OpenReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer xl.Close()
		var all strings.Builder
		for _, sh := range xl.GetSheetList() {
			rows, _ := xl.GetRows(sh, excelize.Options{RawCellValue: true})
			for _, r := range rows {
				all.WriteString(strings.Join(r, "|") + "\n")
			}
		}
		return all.String()
	}
	low, high := cells(1), cells(3)
	for _, h := range []string{"Revenue (EGP)", "Total Revenue", "Fee"} {
		if strings.Contains(low, h) {
			t.Errorf("permission-1 export has %q", h)
		}
	}
	if !strings.Contains(high, "Revenue (EGP)") || !strings.Contains(high, "24,805.00") {
		t.Errorf("permission-3 export lacks the stored revenue (Watanya May base 24,805.00):\n%.2000s", high)
	}
}

/* ------------------------------------------------------------------------ */
/* Statistics read what was stored                                           */
/* ------------------------------------------------------------------------ */

func f2(v any) string {
	f, _ := v.(float64)
	return fmt.Sprintf("%.2f", f)
}

// TestStatisticsEqualStoredPricing: GET /api/trips/statistics over the
// fixture reports apex-rust's golden numbers (the stage-A parity figures),
// and every total equals the sum of the stored per-trip values and
// truck-month rental.
func TestStatisticsEqualStoredPricing(t *testing.T) {
	s := newSurfaces(t)
	code, body := s.json("GET", "/api/trips/statistics?start_date=2025-05-01&end_date=2025-06-30", 3, nil)
	if code != 200 {
		t.Fatalf("statistics = %d %v", code, body)
	}
	byCompany := map[string]map[string]any{}
	for _, c := range body["data"].([]any) {
		c := c.(map[string]any)
		byCompany[c["company"].(string)] = c
	}
	details := func(company string) string {
		var lines []string
		for _, d := range byCompany[company]["details"].([]any) {
			d := d.(map[string]any)
			lines = append(lines, fmt.Sprintf("%v dist=%s rev=%s rental=%s vat=%s total=%s",
				d["group_name"], f2(d["total_distance"]), f2(d["total_revenue"]), f2(d["car_rental"]),
				f2(d["vat"]), f2(d["total_with_vat"])))
		}
		return strings.Join(lines, "\n")
	}
	want := map[string]string{
		"Petrol Arrows": "PA-Far dist=340.00 rev=2212.50 rental=0.00 vat=0.00 total=2212.50\n" +
			"PA-Near dist=240.00 rev=2318.00 rental=0.00 vat=0.00 total=2318.00\n" +
			"PA-Unmapped dist=0.00 rev=0.00 rental=0.00 vat=0.00 total=0.00",
		"Watanya": "Fee 1 dist=400.00 rev=7315.00 rental=0.00 vat=1024.10 total=8339.10\n" +
			"Fee 2 dist=150.00 rev=4884.00 rental=0.00 vat=683.76 total=5567.76\n" +
			"Fee 15 dist=900.00 rev=12606.00 rental=0.00 vat=1764.84 total=14370.84\n" +
			"Fee 0 dist=0.00 rev=0.00 rental=0.00 vat=0.00 total=0.00",
		"TAQA":     "TQ-T1 dist=8610.00 rev=434805.00 rental=62964.29 vat=69687.70 total=567456.99",
		"Petromin": "PM-T1 dist=720.00 rev=30600.00 rental=6000.00 vat=5124.00 total=41724.00",
	}
	for company, w := range want {
		if got := details(company); got != w {
			t.Errorf("%s details:\n--- got ---\n%s\n--- want (apex-rust golden) ---\n%s", company, got, w)
		}
	}

	// Company totals are the stored sums.
	var stored []struct {
		Company               string
		Base, VAT, Rent, RVAT float64
	}
	s.db.Raw(`
        SELECT t.company, SUM(COALESCE(t.base_revenue, 0))::float8 AS base,
               SUM(COALESCE(t.revenue_vat, 0))::float8 AS vat,
               COALESCE((SELECT SUM(rental) FROM truck_month_charges m WHERE m.company = t.company), 0)::float8 AS rent,
               COALESCE((SELECT SUM(rental_vat) FROM truck_month_charges m WHERE m.company = t.company), 0)::float8 AS rvat
          FROM trips t WHERE t.deleted_at IS NULL GROUP BY t.company`).Scan(&stored)
	var wantBase, wantRent float64
	for _, st := range stored {
		c := byCompany[st.Company]
		if f2(c["total_revenue"]) != f2(st.Base) || f2(c["total_car_rent"]) != f2(st.Rent) ||
			f2(c["total_vat"]) != f2(st.VAT+st.RVAT) || f2(c["total_amount"]) != f2(st.Base+st.Rent+st.VAT+st.RVAT) {
			t.Errorf("%s totals %v / %v / %v / %v; stored base %.2f rental %.2f vat %.2f",
				st.Company, c["total_revenue"], c["total_car_rent"], c["total_vat"], c["total_amount"],
				st.Base, st.Rent, st.VAT+st.RVAT)
		}
		wantBase += st.Base
		wantRent += st.Rent
	}

	// Routes and cars add up to their company; the daily breakdown adds up to
	// everything; car totals add up to the base revenue and rental.
	var dayTotal, amount float64
	for _, d := range body["statsByDate"].([]any) {
		dayTotal += d.(map[string]any)["total_revenue"].(float64)
	}
	for company, c := range byCompany {
		amount += c["total_amount"].(float64)
		var routeBase, carBase, carRent float64
		for _, r := range c["route_details"].([]any) {
			r := r.(map[string]any)
			v, _ := r["total_revenue"].(float64)
			routeBase += v
			for _, car := range r["cars"].([]any) {
				car := car.(map[string]any)
				b, _ := car["total_revenue"].(float64)
				rr, _ := car["car_rental"].(float64)
				carBase += b
				carRent += rr
			}
		}
		if f2(routeBase) != f2(c["total_revenue"]) || f2(carBase) != f2(c["total_revenue"]) ||
			f2(carRent) != f2(c["total_car_rent"]) {
			t.Errorf("%s: routes %.2f, cars %.2f / rent %.2f; company %v / %v", company, routeBase, carBase, carRent,
				c["total_revenue"], c["total_car_rent"])
		}
	}
	if f2(dayTotal) != f2(amount) {
		t.Errorf("statsByDate adds to %.2f; companies' total_amount %.2f", dayTotal, amount)
	}
	var ctBase, ctRent float64
	for _, ct := range body["carTotals"].([]any) {
		ct := ct.(map[string]any)
		ctBase += ct["base_revenue"].(float64)
		ctRent += ct["rent"].(float64)
	}
	if f2(ctBase) != f2(wantBase) || f2(ctRent) != f2(wantRent) {
		t.Errorf("carTotals base %.2f rent %.2f; stored %.2f / %.2f", ctBase, ctRent, wantBase, wantRent)
	}

	// May alone: TAQA's May truck-months, and its per-day share sums to them.
	code, body = s.json("GET", "/api/trips/statistics?start_date=2025-05-01&end_date=2025-05-31&company=TAQA", 3, nil)
	if code != 200 {
		t.Fatal(code)
	}
	taqa := body["data"].([]any)[0].(map[string]any)
	if f2(taqa["total_car_rent"]) != "58357.14" {
		t.Errorf("TAQA May rental = %v; want 58357.14 (truck_month_charges)", taqa["total_car_rent"])
	}
	var days float64
	for _, d := range body["statsByDate"].([]any) {
		for _, cd := range d.(map[string]any)["company_details"].([]any) {
			days += cd.(map[string]any)["car_rental"].(float64)
		}
	}
	if f2(days) != "58357.14" {
		t.Errorf("TAQA May daily rental adds to %.2f; want 58357.14", days)
	}
	// Half of June: TQ-A worked June 1-3, so a range to June 2 gets 2/3 of it.
	code, body = s.json("GET", "/api/trips/statistics?start_date=2025-06-01&end_date=2025-06-02&company=TAQA", 3, nil)
	if code != 200 {
		t.Fatal(code)
	}
	var june float64
	s.db.Raw(`SELECT SUM(rental)::float8 FROM truck_month_charges WHERE company = 'TAQA' AND month = '2025-06-01'`).Scan(&june)
	if got := body["data"].([]any)[0].(map[string]any)["total_car_rent"]; f2(got) != f2(june*2/3) {
		t.Errorf("TAQA June 1-2 rental = %v; want 2/3 of %.2f", got, june)
	}
}

// The other surfaces that report money read the same stored figures.
func TestOtherSurfacesReadStoredPricing(t *testing.T) {
	s := newSurfaces(t)

	// Watanya driver analytics: revenue is VAT-inclusive, as it always was.
	code, body := s.json("GET", "/api/trips/watanya/driver-analytics?start_date=2025-05-01&end_date=2025-05-31", 3, nil)
	if code != 200 {
		t.Fatalf("analytics = %d %v", code, body)
	}
	g := body["data"].(map[string]any)["global_stats"].(map[string]any)
	// 7315 + 4884 + 12606 base, + 1024.10 + 683.76 + 1764.84 VAT.
	if f2(g["total_revenue"]) != "28277.70" || g["total_trips"] != 4.0 {
		t.Errorf("analytics global = %v; want 28277.70 over 4 mapped trips (the three-container parent is one)", g)
	}

	// The Watanya report bills the stored base, excluding VAT.
	code, raw := s.do("POST", "/api/trips/watanya/export_report", 3,
		map[string]string{"start_date": "2025-05-01", "end_date": "2025-05-31"})
	if code != 200 {
		t.Fatalf("report = %d %.200s", code, raw)
	}
	xl, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := xl.GetRows(xl.GetSheetList()[0], excelize.Options{RawCellValue: true})
	found := false
	for _, r := range rows {
		if len(r) > 8 && r[0] == "Grand total" {
			found = true
			if amt, _ := strconv.ParseFloat(r[8], 64); fmt.Sprintf("%.2f", amt) != "24805.00" {
				t.Errorf("report grand total = %s; want 24805.00", r[8])
			}
		}
	}
	xl.Close()
	if !found {
		t.Error("report has no grand total row")
	}

	// Receipt piles: one drop-off's cost is its trips' stored base revenue.
	code, body = s.json("GET", "/api/receipt-piles/drop-off?start_date=2025-05-01&end_date=2025-05-31&drop_off_point=WA-B1", 3, nil)
	if code != 200 || f2(body["total_cost"]) != "7315.00" {
		t.Errorf("pile drop-off = %d total_cost %v; want 7315.00", code, body["total_cost"])
	}
	tables := body["terminals"].([]any)
	if rate := tables[0].(map[string]any)["actual_fee"]; f2(rate) != "104.50" {
		t.Errorf("pile rate = %v; want band 1's 104.50", rate)
	}

	// The widget's revenue is the stored base revenue of every live trip.
	var base float64
	s.db.Raw(`SELECT SUM(base_revenue)::float8 FROM trips WHERE deleted_at IS NULL`).Scan(&base)
	code, body = s.json("GET", "/api/stats/widget-data", 3, nil)
	if code != 200 || f2(body["stats"].(map[string]any)["total_revenue"]) != f2(base) {
		t.Errorf("widget = %d %v; want total_revenue %.2f", code, body, base)
	}

	// The drop-off picker quotes what the engine stores for the same trip.
	code, body = s.json("GET", "/api/mappings/dropoffs/"+url.PathEscape("Petrol Arrows")+"/PA-T1?tank_capacity=40000&date=2025-05-05", 3, nil)
	if code != 200 {
		t.Fatalf("picker = %d %v", code, body)
	}
	var pa1 float64
	s.db.Raw(`SELECT base_revenue::float8 FROM trips WHERE receipt_no = 'PA1'`).Scan(&pa1)
	near := body["mappings"].(map[string]any)["PA-Near"].(map[string]any)
	if f2(near["expected_revenue"]) != f2(pa1) || near["fee"] != 30.5 {
		t.Errorf("picker PA-Near = %v; want expected_revenue %.2f (trip PA1, 40000 L) and the fee", near, pa1)
	}
	for _, c := range []struct{ company, terminal, drop, want string }{
		{"Watanya", "WA-T1", "WA-B1", "4180.00"},    // 40000 L x band 1 (104.5) / 1000
		{"TAQA", "TQ-T1", "TQ-Site", "10605.00"},    // 210 km x 50.5
		{"Petromin", "PM-T1", "PM-Site", "7650.00"}, // 180 km x 42.5
	} {
		_, body = s.json("GET", "/api/mappings/dropoffs/"+c.company+"/"+c.terminal+"?tank_capacity=40000", 3, nil)
		m := body["mappings"].(map[string]any)[c.drop].(map[string]any)
		if f2(m["expected_revenue"]) != c.want {
			t.Errorf("%s quote = %v; want %s", c.company, m, c.want)
		}
	}
	// Below permission 3 the picker quotes nothing.
	_, body = s.json("GET", "/api/mappings/dropoffs/Watanya/WA-T1?tank_capacity=40000", 1, nil)
	if _, ok := body["mappings"].(map[string]any)["WA-B1"].(map[string]any)["expected_revenue"]; ok {
		t.Error("the picker quoted revenue at permission 1")
	}
	if code, _ := s.do("GET", "/api/mappings/dropoffs/Watanya/WA-T1?tank_capacity=lots", 3, nil); code != 400 {
		t.Errorf("bad tank_capacity = %d; want 400", code)
	}
}

/* ------------------------------------------------------------------------ */
/* Route coverage                                                            */
/* ------------------------------------------------------------------------ */

func handlerName(h fiber.Handler) string {
	return runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
}

// isMask: MaskMoney's and StripMoney's closure, under whatever name the
// compiler gives it (inlined into SetupRoutes, it is
// SetupRoutes.MaskMoney.maskMoney.funcN).
func isMask(h fiber.Handler) bool {
	return strings.Contains(handlerName(h), ".maskMoney.func")
}

// TestEveryTripSurfaceIsMasked walks the real route table: every route whose
// handler serves trips, fee mappings, receipt piles, receipts or statistics
// must pass through MaskMoney (or StripMoney) on the way out.
func TestEveryTripSurfaceIsMasked(t *testing.T) {
	app := fiber.New()
	SetupRoutes(app, nil)
	routes := app.GetRoutes()

	// A group's middleware is registered, per method, on the group's prefix.
	masked := map[string]bool{} // method + " " + path
	for _, r := range routes {
		for _, h := range r.Handlers {
			if isMask(h) {
				masked[r.Method+" "+strings.TrimSuffix(r.Path, "/")] = true
			}
		}
	}
	covered := func(r fiber.Route) bool {
		p := strings.TrimSuffix(r.Path, "/")
		for {
			if masked[r.Method+" "+p] {
				return true
			}
			i := strings.LastIndex(p, "/")
			if i <= 0 {
				return false
			}
			p = p[:i]
		}
	}
	serves := []string{"(*TripHandler)", "(*FeeMappingHandler)", "(*ReceiptPilesHandler)",
		"(*ReceiptController)", "TripSummary"}
	checked := 0
	for _, r := range routes {
		if r.Method == "HEAD" || len(r.Handlers) == 0 {
			continue
		}
		final := handlerName(r.Handlers[len(r.Handlers)-1])
		for _, s := range serves {
			if strings.Contains(final, s) {
				checked++
				if !covered(r) {
					t.Errorf("%s %s (%s) is not behind MaskMoney", r.Method, r.Path, final)
				}
			}
		}
	}
	if checked < 40 {
		t.Errorf("only %d trip surfaces found; the handler names this test matches on have moved", checked)
	}
}

// The driver app shows no money: GetDriverTrips no longer adds up fees into
// a total_revenue, and StripMoney (on both driver groups) zeroes the stored
// revenue its trips carry, whoever calls.
func TestDriverTripsCarryNoMoney(t *testing.T) {
	s := newSurfaces(t)
	s.db.Exec(`UPDATE parent_trips SET driver_id = 77 WHERE id = 9001`)
	th := Controllers.NewTripHandler(s.db)
	asDriver := func(c *fiber.Ctx) error {
		c.Locals("driver", Models.Driver{Model: gorm.Model{ID: 77}})
		return c.Next()
	}
	for _, strip := range []bool{false, true} {
		app := fiber.New()
		if strip {
			app.Get("/trips", asDriver, middleware.StripMoney(), th.GetDriverTrips)
		} else {
			app.Get("/trips", asDriver, th.GetDriverTrips)
		}
		resp, err := app.Test(httptest.NewRequest("GET", "/trips", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		body := map[string]any{}
		if err := json.Unmarshal(raw, &body); err != nil || resp.StatusCode != 200 {
			t.Fatalf("driver trips = %d %.300s", resp.StatusCode, raw)
		}
		data := body["data"].([]any)
		if len(data) != 1 {
			t.Fatalf("driver trips = %v; want parent 9001", data)
		}
		parent := data[0].(map[string]any)
		if parent["total_revenue"] != 0.0 {
			t.Errorf("total_revenue = %v; the handler still adds up fees", parent["total_revenue"])
		}
		for _, ct := range parent["container_trips"].([]any) {
			if fee := ct.(map[string]any)["fee"]; fee != 0.0 {
				t.Errorf("container fee = %v; want none", fee)
			}
		}
		if got := nonZeroMoney(body); strip && len(got) > 0 {
			t.Errorf("driver app leaks money: %v", got)
		} else if !strip && len(got) == 0 {
			t.Error("without StripMoney the stored revenue should still be there; the check proves nothing")
		}
	}
}
