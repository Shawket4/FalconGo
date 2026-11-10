package Apis

import (
	"Falcon/Models"
	"Falcon/documents"
	"Falcon/testutil"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func carApp(t *testing.T) *fiber.App {
	t.Helper()
	db := testutil.DocumentsSchema(t)
	prev := Models.DB
	Models.DB = db
	t.Cleanup(func() { Models.DB = prev })
	app := fiber.New()
	app.Post("/api/cars", RegisterCar)
	app.Put("/api/cars/:id", UpdateCar)
	return app
}

func sendCar(t *testing.T, app *fiber.App, method, path string, body map[string]any, asForm bool) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	if asForm {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.WriteField("request", string(raw))
		w.Close()
		req = httptest.NewRequest(method, path, &buf)
		req.Header.Set(fiber.HeaderContentType, w.FormDataContentType())
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func carExpiry(t *testing.T, carID uint, typeKey string) string {
	t.Helper()
	docs, err := documents.ListOwner(Models.DB, documents.OwnerCar, carID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d.TypeKey == typeKey && d.Versions[0].ExpiryDate != nil {
			return *d.Versions[0].ExpiryDate
		}
	}
	return ""
}

// A car needs a plate and nothing else; what the licence says about the
// vehicle and its trailer is stored when sent.
func TestRegisterCarTakesOnlyAPlateAndStoresTheVehicleIdentity(t *testing.T) {
	app := carApp(t)

	if code, _ := sendCar(t, app, "POST", "/api/cars", map[string]any{"car_type": "Trailer"}, false); code != 400 {
		t.Errorf("a car with no plate = %d; want 400", code)
	}
	code, body := sendCar(t, app, "POST", "/api/cars", map[string]any{"car_no_plate": " ABC 123 "}, false)
	if code != 200 {
		t.Fatalf("a car with only a plate = %d %v; want 200", code, body)
	}
	var bare Models.Car
	Models.DB.First(&bare, uint(body["car_id"].(float64)))
	if bare.CarNoPlate != "ABC 123" || bare.Transporter != "Apex" || !bare.IsApproved || bare.EngineCC != 0 || bare.EngineCylinders != 0 || bare.WeightTons != 0 || bare.TrailerWeightTons != 0 || bare.TrailerNoPlate != "" {
		t.Errorf("bare car = %+v", bare)
	}
	if docs, _ := documents.ListOwner(Models.DB, documents.OwnerCar, bare.ID, true); len(docs) != 0 {
		t.Errorf("a car with no dates got %d documents", len(docs))
	}

	code, body = sendCar(t, app, "POST", "/api/cars", map[string]any{
		"car_no_plate": "XYZ 789", "car_type": "Trailer", "tank_capacity": 42000, "compartments": []int{14000, 14000, 14000},
		"chassis_no": " WDB9340321L ", "engine_no": "OM471-55", "engine_cc": 12800, "engine_cylinders": 6, "weight_tons": 9.3, "payload_tons": 18.5, "color": "White",
		"trailer_no_plate": "TR 4521", "trailer_chassis_no": "NP9TS3", "trailer_weight_tons": 15.8, "trailer_payload_tons": 32, "trailer_color": "Silver",
		"license_expiration_date": "2026-11-30",
	}, false)
	if code != 200 {
		t.Fatalf("a full car = %d %v; want 200", code, body)
	}
	var full Models.Car
	Models.DB.First(&full, uint(body["car_id"].(float64)))
	if full.ChassisNo != "WDB9340321L" && strings.TrimSpace(full.ChassisNo) != "WDB9340321L" {
		t.Errorf("chassis = %q", full.ChassisNo)
	}
	if full.EngineNo != "OM471-55" || full.EngineCC != 12800 || full.EngineCylinders != 6 || full.WeightTons != 9.3 || full.TrailerWeightTons != 15.8 || full.PayloadTons != 18.5 || full.Color != "White" ||
		full.TrailerNoPlate != "TR 4521" || full.TrailerChassisNo != "NP9TS3" || full.TrailerPayloadTons != 32 || full.TrailerColor != "Silver" {
		t.Errorf("vehicle identity = %+v", full)
	}
	if got := carExpiry(t, full.ID, "vehicle_licence"); got != "2026-11-30" {
		t.Errorf("vehicle licence expiry = %q; want 2026-11-30", got)
	}

	for name, bad := range map[string]map[string]any{
		"a negative engine size":    {"car_no_plate": "N1", "engine_cc": -1},
		"negative cylinders":        {"car_no_plate": "N4", "engine_cylinders": -6},
		"a negative weight":         {"car_no_plate": "N5", "weight_tons": -9},
		"a negative trailer weight": {"car_no_plate": "N6", "trailer_weight_tons": -0.5},
		"a negative payload":        {"car_no_plate": "N2", "trailer_payload_tons": -3},
		"a date that is not one":    {"car_no_plate": "N3", "calibration_expiration_date": "tomorrow"},
	} {
		if code, _ := sendCar(t, app, "POST", "/api/cars", bad, false); code != 400 {
			t.Errorf("%s = %d; want 400", name, code)
		}
	}
	var n int64
	Models.DB.Model(&Models.Car{}).Count(&n)
	if n != 2 {
		t.Errorf("cars = %d; want the 2 accepted", n)
	}
}

// An edit changes what it sends and nothing else, whichever way it arrives:
// the dashboard's JSON, the advisor's multipart "request", or the old POST
// that names the car by ID.
func TestUpdateCarChangesOnlyWhatIsSent(t *testing.T) {
	app := carApp(t)
	layout, _ := json.Marshal([]int{10000, 20000})
	car := Models.Car{CarNoPlate: "ABC 123", CarType: "Trailer", Transporter: "Apex", TankCapacity: 30000,
		JSONCompartments: layout, ChassisNo: "CH-1", TrailerNoPlate: "TR 1", OperatingCompany: "watanya",
		CalibrationExpirationDate: "2026-02-02"}
	if err := Models.DB.Create(&car).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/cars/1"
	reload := func() Models.Car {
		var c Models.Car
		Models.DB.First(&c, car.ID)
		return c
	}

	if code, body := sendCar(t, app, "PUT", path, map[string]any{"engine_no": "EN-9", "engine_cc": 11000, "engine_cylinders": 8, "weight_tons": 7.7, "trailer_weight_tons": 14, "color": "Red"}, false); code != 200 {
		t.Fatalf("json edit = %d %v", code, body)
	}
	got := reload()
	if got.EngineNo != "EN-9" || got.EngineCC != 11000 || got.EngineCylinders != 8 || got.WeightTons != 7.7 || got.TrailerWeightTons != 14 || got.Color != "Red" {
		t.Errorf("sent fields not stored: %+v", got)
	}
	if got.CarNoPlate != "ABC 123" || got.ChassisNo != "CH-1" || got.TrailerNoPlate != "TR 1" || got.TankCapacity != 30000 ||
		string(got.JSONCompartments) == "null" || got.OperatingCompany != "watanya" || got.CalibrationExpirationDate != "2026-02-02" {
		t.Errorf("an unsent column changed: %+v", got)
	}
	// The calibration date that was on the row is now a document too.
	if exp := carExpiry(t, car.ID, "calibration_certificate"); exp != "2026-02-02" {
		t.Errorf("calibration expiry = %q; want the row's 2026-02-02", exp)
	}

	// The advisor: multipart, whole row, the id in the body.
	code, _ := sendCar(t, app, "PUT", path, map[string]any{
		"ID": car.ID, "car_no_plate": "ABC 123", "car_type": "Trailer", "tank_capacity": 30000,
		"license_expiration_date": "2027-05-05", "calibration_expiration_date": "2027-06-06", "tank_license_expiration_date": "",
	}, true)
	if code != 200 {
		t.Fatalf("advisor edit = %d", code)
	}
	got = reload()
	if got.LicenseExpirationDate != "2027-05-05" || got.EngineNo != "EN-9" || got.EngineCylinders != 8 || got.WeightTons != 7.7 || got.TrailerWeightTons != 14 || got.ChassisNo != "CH-1" {
		t.Errorf("after the advisor's edit: %+v", got)
	}
	if lic, cal := carExpiry(t, car.ID, "vehicle_licence"), carExpiry(t, car.ID, "calibration_certificate"); lic != "2027-05-05" || cal != "2027-06-06" {
		t.Errorf("documents = licence %q, calibration %q", lic, cal)
	}
	docs, _ := documents.ListOwner(Models.DB, documents.OwnerCar, car.ID, true)
	for _, d := range docs {
		if len(d.Versions) != 1 {
			t.Errorf("%s has %d versions; a changed date is a correction, not a renewal", d.TypeKey, len(d.Versions))
		}
	}

	// The old upsert: POST with an ID edits, and no longer blanks the rest.
	if code, _ := sendCar(t, app, "POST", "/api/cars", map[string]any{"ID": car.ID, "trailer_no_plate": "TR 2"}, false); code != 200 {
		t.Fatalf("post with ID = %d", code)
	}
	got = reload()
	if got.TrailerNoPlate != "TR 2" || got.CarNoPlate != "ABC 123" || got.EngineNo != "EN-9" || got.Transporter != "Apex" {
		t.Errorf("after a POST with ID: %+v", got)
	}
	var n int64
	Models.DB.Model(&Models.Car{}).Count(&n)
	if n != 1 {
		t.Errorf("cars = %d; the POST with an ID made a new one", n)
	}

	// Clearing is sending the empty value.
	sendCar(t, app, "PUT", path, map[string]any{"chassis_no": "", "engine_cc": 0, "engine_cylinders": 0, "weight_tons": 0, "compartments": []int{}}, false)
	if got = reload(); got.ChassisNo != "" || got.EngineCC != 0 || got.EngineCylinders != 0 || got.WeightTons != 0 || got.TrailerWeightTons != 14 || string(got.JSONCompartments) != "[]" {
		t.Errorf("cleared fields = %q, %d, %s", got.ChassisNo, got.EngineCC, got.JSONCompartments)
	}

	for name, tc := range map[string]struct {
		path string
		body map[string]any
		want int
	}{
		"a blank plate":      {path, map[string]any{"car_no_plate": " "}, 400},
		"a negative payload": {path, map[string]any{"payload_tons": -1}, 400},
		"negative cylinders": {path, map[string]any{"engine_cylinders": -1}, 400},
		"a negative weight":  {path, map[string]any{"trailer_weight_tons": -1}, 400},
		"a bad date":         {path, map[string]any{"license_expiration_date": "05/05/2027"}, 400},
		"a missing car":      {"/api/cars/999", map[string]any{"color": "Blue"}, 404},
	} {
		if code, _ := sendCar(t, app, "PUT", tc.path, tc.body, false); code != tc.want {
			t.Errorf("%s = %d; want %d", name, code, tc.want)
		}
	}
	if got = reload(); got.LicenseExpirationDate != "2027-05-05" {
		t.Errorf("a refused edit changed the licence date to %q", got.LicenseExpirationDate)
	}
}
