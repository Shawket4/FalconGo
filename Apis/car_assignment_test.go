package Apis

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func strPtr(s string) *string { return &s }

func TestCarAssignmentColumnsOnlyTakesWhatIsSent(t *testing.T) {
	got, err := carAssignmentInput{OperatingCompany: strPtr("  watanya ")}.columns()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["operating_company"] != "watanya" {
		t.Errorf("columns() = %v; want only a trimmed operating_company", got)
	}

	got, err = carAssignmentInput{OperatingCompany: strPtr("petrol"), OperatingArea: strPtr("Cairo ")}.columns()
	if err != nil || len(got) != 2 || got["operating_area"] != "Cairo" {
		t.Errorf("columns() = %v, %v; want both, trimmed", got, err)
	}
}

func TestCarAssignmentColumnsRefusesEmptyAndBlank(t *testing.T) {
	for name, in := range map[string]carAssignmentInput{
		"neither field": {},
		"blank company": {OperatingCompany: strPtr("   ")},
		"blank area":    {OperatingCompany: strPtr("watanya"), OperatingArea: strPtr("")},
	} {
		if _, err := in.columns(); err == nil {
			t.Errorf("%s: columns() accepted it", name)
		}
	}
}

// The refusals that need no database: a bad id and a body that asks for nothing.
func TestUpdateCarAssignmentRejectsBadRequestsBeforeTheDatabase(t *testing.T) {
	app := fiber.New()
	app.Patch("/api/cars/:id/assignment", UpdateCarAssignment)

	for name, tc := range map[string]struct{ path, body string }{
		"non-numeric id": {"/api/cars/abc/assignment", `{"operating_area":"x"}`},
		"zero id":        {"/api/cars/0/assignment", `{"operating_area":"x"}`},
		"empty object":   {"/api/cars/1/assignment", `{}`},
		"malformed body": {"/api/cars/1/assignment", `{"operating_area":`},
		"wrong type":     {"/api/cars/1/assignment", `{"operating_area":5}`},
	} {
		req := httptest.NewRequest("PATCH", tc.path, strings.NewReader(tc.body))
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("%s: status %d; want 400", name, resp.StatusCode)
		}
	}
}
