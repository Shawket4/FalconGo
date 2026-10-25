package Controllers

import (
	"Falcon/Models"
	"Falcon/testutil"
	"encoding/json"
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
	db := testutil.PGSchema(t)
	if err := db.AutoMigrate(&Models.Driver{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
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
