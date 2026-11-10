package Controllers

import (
	"Falcon/Models"
	"Falcon/documents"
	"Falcon/testutil"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func putDriver(t *testing.T, app *fiber.App, pathID string, body map[string]any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	form := url.Values{"request": {string(b)}}.Encode()
	req := httptest.NewRequest("PUT", "/api/drivers/"+pathID, strings.NewReader(form))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestUpdateDriverKeepsThePINAndNeverInserts(t *testing.T) {
	db := testutil.DocumentsSchema(t)
	prev := Models.DB
	Models.DB = db
	t.Cleanup(func() { Models.DB = prev })

	pin := "$2a$hashed-pin"
	driver := Models.Driver{Name: "Old Name", MobileNumber: "010", PIN: &pin, Transporter: "Apex"}
	if err := db.Create(&driver).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	app := fiber.New()
	app.Put("/api/drivers/:id", UpdateDriver)

	if code := putDriver(t, app, "1", map[string]any{"ID": driver.ID, "name": "New Name", "mobile_number": "011"}); code != 200 {
		t.Fatalf("update = %d; want 200", code)
	}
	var after Models.Driver
	db.First(&after, driver.ID)
	if after.Name != "New Name" || after.MobileNumber != "011" {
		t.Errorf("fields not updated: %+v", after)
	}
	if after.PIN == nil || *after.PIN != pin {
		t.Errorf("PIN = %v; want it kept (%q)", after.PIN, pin)
	}
	if after.Transporter != "Apex" {
		t.Errorf("transporter = %q; an unedited column changed", after.Transporter)
	}

	// The route id is used when the body carries none.
	if code := putDriver(t, app, "1", map[string]any{"name": "Via Path"}); code != 200 {
		t.Fatalf("update by path id = %d; want 200", code)
	}

	// Missing and zero ids are 404s, never a new driver.
	if code := putDriver(t, app, "999", map[string]any{"ID": 999, "name": "Ghost"}); code != 404 {
		t.Errorf("missing id = %d; want 404", code)
	}
	if code := putDriver(t, app, "0", map[string]any{"name": "Ghost"}); code != 404 {
		t.Errorf("zero id = %d; want 404", code)
	}
	var n int64
	db.Model(&Models.Driver{}).Count(&n)
	if n != 1 {
		t.Errorf("drivers = %d; want 1", n)
	}
}

// The advisor edits a driver with a whole row, dates included, as a multipart
// "request" on the path it has always used. The dates land in the documents;
// fields it leaves out stay as they are.
func TestUpdateDriverTakesDatesIntoDocuments(t *testing.T) {
	db := testutil.DocumentsSchema(t)
	prev := Models.DB
	Models.DB = db
	t.Cleanup(func() { Models.DB = prev })

	driver := Models.Driver{Name: "Hassan", MobileNumber: "010", Transporter: "Apex", DrugTestExpirationDate: "2026-01-01"}
	if err := db.Create(&driver).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	app := fiber.New()
	app.Put("/api/drivers/documents/:id", UpdateDriver)
	app.Post("/api/drivers", RegisterDriver)

	send := func(method, path string, body map[string]any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.WriteField("request", string(b))
		w.Close()
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set(fiber.HeaderContentType, w.FormDataContentType())
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	expiry := func(ownerID uint, typeKey string) string {
		t.Helper()
		docs, err := documents.ListOwner(db, documents.OwnerDriver, ownerID, true)
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

	code, _ := send("PUT", "/api/drivers/documents/1", map[string]any{
		"ID": driver.ID, "name": "Hassan Ali", "driver_license_expiration_date": "2027-03-04",
	})
	if code != 200 {
		t.Fatalf("update = %d; want 200", code)
	}
	var after Models.Driver
	db.First(&after, driver.ID)
	if after.Name != "Hassan Ali" || after.MobileNumber != "010" || after.DriverLicenseExpirationDate != "2027-03-04" {
		t.Errorf("driver after update = %+v", after)
	}
	if got := expiry(driver.ID, "driving_licence"); got != "2027-03-04" {
		t.Errorf("driving licence expiry = %q; want 2027-03-04", got)
	}
	// The date that was already on the row, and was not sent, is adopted too
	// rather than lost.
	if got := expiry(driver.ID, "drug_test"); got != "2026-01-01" || after.DrugTestExpirationDate != "2026-01-01" {
		t.Errorf("drug test expiry = %q, column %q; want the 2026-01-01 it had", got, after.DrugTestExpirationDate)
	}

	if code, body := send("PUT", "/api/drivers/documents/1", map[string]any{"ID": driver.ID, "drug_test_expiration_date": "next year"}); code != 400 || body["code"] != "invalid_date" {
		t.Errorf("a date that is not one = %d %v; want 400 invalid_date", code, body)
	}
	if code, _ := send("PUT", "/api/drivers/documents/1", map[string]any{"ID": driver.ID, "name": "  "}); code != 400 {
		t.Errorf("a blank name = %d; want 400", code)
	}

	// Registering needs a name and nothing else.
	if code, _ := send("POST", "/api/drivers", map[string]any{"mobile_number": "012"}); code != 400 {
		t.Errorf("a driver with no name = %d; want 400", code)
	}
	code, body := send("POST", "/api/drivers", map[string]any{"name": "Sayed", "safety_license_expiration_date": "2026-08-08"})
	if code != 200 {
		t.Fatalf("register = %d %v; want 200", code, body)
	}
	id := uint(body["driver_id"].(float64))
	if got := expiry(id, "safety_certificate"); got != "2026-08-08" {
		t.Errorf("new driver's safety certificate expiry = %q; want 2026-08-08", got)
	}
}
