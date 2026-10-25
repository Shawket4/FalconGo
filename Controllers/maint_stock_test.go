package Controllers

import (
	"Falcon/Constants"
	"Falcon/Models"
	"Falcon/middleware"
	"Falcon/testutil"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func ptr[T any](v T) *T { return &v }

func TestValidateMaintStockKindsAndAmounts(t *testing.T) {
	ref := uuid.NewString()
	ok := []maintStockInput{
		{Kind: "tire_debit", Qty: ptr(1), RefID: ref},
		{Kind: "tire_credit", Qty: ptr(4), RefID: ref},
		{Kind: "oil_debit", Liters: ptr(0.5), RefID: ref},
		{Kind: "oil_credit", Liters: ptr(200.0), RefID: ref, WorkOrderID: ptr(uuid.NewString())},
		{Kind: "oil_credit", Liters: ptr(1.0), RefID: ref, WorkOrderID: ptr("")},
	}
	for _, in := range ok {
		if _, problem := validateMaintStock(in); problem != "" {
			t.Errorf("%+v refused: %s", in, problem)
		}
	}
	bad := map[string]maintStockInput{
		"unknown kind":        {Kind: "adjustment", Qty: ptr(1), RefID: ref},
		"empty kind":          {Qty: ptr(1), RefID: ref},
		"tire without qty":    {Kind: "tire_debit", RefID: ref},
		"tire zero qty":       {Kind: "tire_credit", Qty: ptr(0), RefID: ref},
		"tire negative qty":   {Kind: "tire_debit", Qty: ptr(-2), RefID: ref},
		"tire liters, no qty": {Kind: "tire_debit", Liters: ptr(3.0), RefID: ref},
		"oil without liters":  {Kind: "oil_debit", Qty: ptr(3), RefID: ref},
		"oil zero liters":     {Kind: "oil_credit", Liters: ptr(0.0), RefID: ref},
		"oil negative liters": {Kind: "oil_debit", Liters: ptr(-1.0), RefID: ref},
		"missing ref_id":      {Kind: "tire_debit", Qty: ptr(1)},
		"bad ref_id":          {Kind: "tire_debit", Qty: ptr(1), RefID: "not-a-uuid"},
		"bad work_order_id":   {Kind: "tire_debit", Qty: ptr(1), RefID: ref, WorkOrderID: ptr("wo-1")},
	}
	for name, in := range bad {
		if _, problem := validateMaintStock(in); problem == "" {
			t.Errorf("%s: accepted %+v", name, in)
		}
	}
}

// maintStockTestDB is a throwaway schema with the tables these routes touch.
func maintStockTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.PGSchema(t)
	if err := db.AutoMigrate(&Models.User{}); err != nil {
		t.Fatalf("users: %v", err)
	}
	if err := Models.MigrateMaintStockMovements(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Twice: the migration runs on every boot.
	if err := Models.MigrateMaintStockMovements(db); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
	return db
}

// maintToken mints the token maint-rust relays: its own HS256 mint, with the
// claim set of its AuthClaims, not one FalconGo issued.
func maintToken(t *testing.T, userID uint, permission int) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_type":  "admin_user",
		"user_id":    userID,
		"driver_id":  0,
		"permission": permission,
		"iss":        fmt.Sprint(userID),
		"exp":        time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(Constants.JWTSecret))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return tok
}

func TestMaintStockRoutesAgainstPostgres(t *testing.T) {
	db := maintStockTestDB(t)
	prevDB, prevSecret := Models.DB, Constants.JWTSecret
	Models.DB, Constants.JWTSecret = db, "maint-stock-test-secret"
	t.Cleanup(func() { Models.DB, Constants.JWTSecret = prevDB, prevSecret })

	user := Models.User{Name: "garage", Email: "garage@example.invalid", Permission: 1}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	token := maintToken(t, user.Id, 1)

	app := fiber.New()
	api := app.Group("/api")
	api.Post("/maint-stock/debit", middleware.Verify(1), RecordMaintStockMovement)
	api.Get("/maint-stock/tires", middleware.Verify(1), GetMaintTireStock)
	api.Get("/maint-stock/oil", middleware.Verify(1), GetMaintOilStock)

	do := func(method, path, body, cookie string) (int, string) {
		t.Helper()
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, r)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		if cookie != "" {
			req.Header.Set("Cookie", "jwt="+cookie)
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	// The payload exactly as maint-rust's mirror builds it: every key present,
	// nulls included.
	movement := func(kind string, brand, model, size, oil any, qty, liters any, ref string) string {
		b, _ := json.Marshal(map[string]any{
			"kind": kind, "brand": brand, "model": model, "size": size, "oil_type": oil,
			"qty": qty, "liters": liters, "ref_id": ref, "work_order_id": nil,
		})
		return string(b)
	}
	post := func(body string, want int) {
		t.Helper()
		if code, resp := do("POST", "/api/maint-stock/debit", body, token); code != want {
			t.Fatalf("POST %s = %d %s; want %d", body, code, resp, want)
		} else if want == 200 && strings.TrimSpace(resp) != `{"ok":true}` {
			t.Fatalf("POST %s body = %s; want {\"ok\":true}", body, resp)
		}
	}

	creditRef, debitRef := uuid.NewString(), uuid.NewString()
	post(movement("tire_credit", "Pirelli", "FR01", "315/80R22.5", nil, 10, nil, creditRef), 200)
	post(movement("tire_debit", "Pirelli", "FR01", "315/80R22.5", nil, 3, nil, debitRef), 200)
	// Replays of both, the credit with a different qty: keyed on ref_id alone.
	post(movement("tire_debit", "Pirelli", "FR01", "315/80R22.5", nil, 3, nil, debitRef), 200)
	post(movement("tire_credit", "Pirelli", "FR01", "315/80R22.5", nil, 99, nil, creditRef), 200)
	// No model: grouped under "", and allowed to go negative.
	post(movement("tire_debit", "Tegrys", nil, "315/80R22.5", nil, 2, nil, uuid.NewString()), 200)

	post(movement("oil_credit", nil, nil, nil, "15W-40", nil, 400.5, uuid.NewString()), 200)
	oilDebit := uuid.NewString()
	post(movement("oil_debit", nil, nil, nil, "15W-40", nil, 20.25, oilDebit), 200)
	post(movement("oil_debit", nil, nil, nil, "15W-40", nil, 20.25, oilDebit), 200)
	post(movement("oil_debit", nil, nil, nil, "80W-90", nil, 1.5, uuid.NewString()), 200)

	// Refused before anything is written.
	post(movement("tire_debit", "Pirelli", "FR01", "315/80R22.5", nil, 0, nil, uuid.NewString()), 400)
	post(movement("oil_debit", nil, nil, nil, "15W-40", nil, -5, uuid.NewString()), 400)
	post(movement("adjustment", "Pirelli", "FR01", "315/80R22.5", nil, 1, nil, uuid.NewString()), 400)
	post(movement("tire_debit", "Pirelli", "FR01", "315/80R22.5", nil, 1, nil, "nope"), 400)
	post(`{not json`, 400)
	if code, _ := do("POST", "/api/maint-stock/debit", movement("tire_debit", "P", "", "", nil, 1, nil, uuid.NewString()), ""); code != fiber.StatusUnauthorized {
		t.Fatalf("POST without a token = %d; want 401", code)
	}

	var count int64
	db.Model(&Models.MaintStockMovement{}).Count(&count)
	if count != 6 {
		t.Fatalf("ledger rows = %d; want 6 (replays and refusals insert nothing)", count)
	}
	var recordedBy uint
	db.Raw("SELECT recorded_by_user_id FROM maint_stock_movements WHERE ref_id = ?", debitRef).Scan(&recordedBy)
	if recordedBy != user.Id {
		t.Errorf("recorded_by_user_id = %d; want %d", recordedBy, user.Id)
	}

	code, body := do("GET", "/api/maint-stock/tires", "", token)
	if code != 200 {
		t.Fatalf("GET tires = %d %s", code, body)
	}
	var tires []map[string]any
	if err := json.Unmarshal([]byte(body), &tires); err != nil {
		t.Fatalf("tires body %s: %v", body, err)
	}
	wantTires := []map[string]any{
		{"brand": "Pirelli", "model": "FR01", "size": "315/80R22.5", "on_hand_qty": 7.0},
		{"brand": "Tegrys", "model": "", "size": "315/80R22.5", "on_hand_qty": -2.0},
	}
	if !reflect.DeepEqual(tires, wantTires) {
		t.Errorf("tires = %v; want %v", tires, wantTires)
	}
	if strings.Contains(body, "7.0") || !strings.Contains(body, `"on_hand_qty":7`) {
		t.Errorf("on_hand_qty must be an integer: %s", body)
	}

	code, body = do("GET", "/api/maint-stock/oil", "", token)
	if code != 200 {
		t.Fatalf("GET oil = %d %s", code, body)
	}
	var oil []map[string]any
	if err := json.Unmarshal([]byte(body), &oil); err != nil {
		t.Fatalf("oil body %s: %v", body, err)
	}
	wantOil := []map[string]any{
		{"oil_type": "15W-40", "liters_on_hand": 380.25},
		{"oil_type": "80W-90", "liters_on_hand": -1.5},
	}
	if !reflect.DeepEqual(oil, wantOil) {
		t.Errorf("oil = %v; want %v", oil, wantOil)
	}
}
