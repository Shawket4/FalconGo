package Apis

import (
	"Falcon/Models"
	"Falcon/testutil"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func postJSON(t *testing.T, app *fiber.App, path string, body map[string]any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

// Refused before the database is touched (Models.DB is not needed).
func TestFuelEventsWithoutFuelAreRefused(t *testing.T) {
	app := fiber.New()
	app.Post("/add", AddFuelEvent)
	app.Post("/edit", EditFuelEvent)
	for _, liters := range []float64{0, -3} {
		if code := postJSON(t, app, "/add", map[string]any{"car_id": 1, "liters": liters}); code != 400 {
			t.Errorf("AddFuelEvent liters=%v = %d; want 400", liters, code)
		}
	}
	if code := postJSON(t, app, "/edit", map[string]any{"car_id": 1, "liters": 10}); code != 404 {
		t.Errorf("EditFuelEvent with no ID = %d; want 404", code)
	}
}

func TestEditsOfMissingRowsAre404sNotInserts(t *testing.T) {
	db := testutil.PGSchema(t)
	if err := db.AutoMigrate(&Models.FuelEvent{}, &Models.OilChange{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Only the columns these handlers read; Car's full model drags in more.
	if err := db.Exec(`CREATE TABLE cars (id bigserial PRIMARY KEY, car_no_plate text, deleted_at timestamptz)`).Error; err != nil {
		t.Fatalf("cars: %v", err)
	}
	prev := Models.DB
	Models.DB = db
	t.Cleanup(func() { Models.DB = prev })

	db.Exec(`INSERT INTO cars (car_no_plate) VALUES ('ABC 123')`)
	gone := Models.FuelEvent{CarID: 1, CarNoPlate: "ABC 123", Liters: 50}
	db.Create(&gone)
	db.Delete(&gone)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error { c.Locals("user", &Models.User{Id: 1, Name: "admin"}); return c.Next() })
	app.Post("/AddFuelEvent", AddFuelEvent)
	app.Post("/EditFuelEvent", EditFuelEvent)
	app.Post("/CreateOilChange", CreateOilChange)
	app.Post("/EditOilChange", EditOilChange)

	cases := []struct {
		path string
		body map[string]any
		want int
	}{
		{"/AddFuelEvent", map[string]any{"car_id": 42, "liters": 10}, 404},
		{"/AddFuelEvent", map[string]any{"liters": 10}, 404},
		{"/EditFuelEvent", map[string]any{"ID": 999, "car_id": 1, "liters": 10}, 404},
		{"/EditFuelEvent", map[string]any{"ID": gone.ID, "car_id": 1, "liters": 10}, 404}, // soft-deleted: not revived
		{"/EditFuelEvent", map[string]any{"ID": 0, "car_id": 1, "liters": 10}, 404},
		{"/CreateOilChange", map[string]any{"car_id": 42}, 404},
		{"/EditOilChange", map[string]any{"ID": 999, "car_id": 1}, 404},
		{"/EditOilChange", map[string]any{"ID": 0, "car_id": 1}, 404},
	}
	for _, tc := range cases {
		if code := postJSON(t, app, tc.path, tc.body); code != tc.want {
			t.Errorf("%s %v = %d; want %d", tc.path, tc.body, code, tc.want)
		}
	}

	var fuel, liveFuel, oil int64
	db.Unscoped().Model(&Models.FuelEvent{}).Count(&fuel)
	db.Model(&Models.FuelEvent{}).Count(&liveFuel)
	db.Unscoped().Model(&Models.OilChange{}).Count(&oil)
	if fuel != 1 || liveFuel != 0 || oil != 0 {
		t.Errorf("rows: fuel=%d (live %d) oil=%d; want 1 (0 live) and 0", fuel, liveFuel, oil)
	}
}
