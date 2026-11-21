package Apis

import (
	"Falcon/Models"
	"Falcon/testutil"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// The dashboard deletes with DELETE /api/drivers/:id and no body. The handler
// used to body-parse the id, so every such delete was a 400.
func TestDeleteDriverByPathParam(t *testing.T) {
	db := testutil.PGSchema(t)
	if err := db.AutoMigrate(&Models.Driver{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := Models.DB
	Models.DB = db
	t.Cleanup(func() { Models.DB = prev })

	keep := Models.Driver{Name: "Keep Me"}
	gone := Models.Driver{Name: "Delete Me"}
	viaBody := Models.Driver{Name: "Old Caller"}
	for _, d := range []*Models.Driver{&keep, &gone, &viaBody} {
		if err := db.Create(d).Error; err != nil {
			t.Fatal(err)
		}
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error { c.Locals("user", &Models.User{Id: 1, Name: "admin", Permission: 3}); return c.Next() })
	app.Delete("/drivers/:id", DeleteDriver)

	do := func(path, body string) int {
		req := httptest.NewRequest("DELETE", path, strings.NewReader(body))
		if body != "" {
			req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	if code := do("/drivers/"+strconv.Itoa(int(gone.ID)), ""); code != 200 {
		t.Fatalf("DELETE by path = %d; want 200", code)
	}
	if code := do("/drivers/"+strconv.Itoa(int(gone.ID)), ""); code != 404 {
		t.Errorf("deleting an already deleted driver = %d; want 404", code)
	}
	if code := do("/drivers/999999", ""); code != 404 {
		t.Errorf("DELETE of a missing driver = %d; want 404", code)
	}
	if code := do("/drivers/x", `{"ID":`+strconv.Itoa(int(viaBody.ID))+`}`); code != 200 {
		t.Errorf("DELETE with a body id (older callers) = %d; want 200", code)
	}
	if code := do("/drivers/x", ""); code != 400 {
		t.Errorf("DELETE with no id at all = %d; want 400", code)
	}

	var left []Models.Driver
	db.Find(&left)
	if len(left) != 1 || left[0].ID != keep.ID {
		t.Fatalf("drivers left = %+v; want only %q", left, keep.Name)
	}
	var soft int64
	db.Unscoped().Model(&Models.Driver{}).Where("deleted_at IS NOT NULL").Count(&soft)
	if soft != 2 {
		t.Errorf("soft-deleted rows = %d; want 2 (a delete keeps the row)", soft)
	}
}
