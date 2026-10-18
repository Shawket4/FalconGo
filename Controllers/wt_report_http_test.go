package Controllers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"Falcon/Models"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
)

// wtApp serves the report over fixture data, through the same request
// parsing and response code as the real route.
func wtApp(trips []Models.TripStruct, maps []Models.FeeMapping) *fiber.App {
	app := fiber.New()
	app.Post("/report", func(c *fiber.Ctx) error {
		var req wtReportRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Cannot parse request body"})
		}
		if err := req.normalise(); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return wtRespond(c, req, trips, maps)
	})
	return app
}

func wtPost(t *testing.T, app *fiber.App, body map[string]string) (int, string, string, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/report", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 30000)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"), data
}

func TestWatanyaExportEmptyRange(t *testing.T) {
	app := wtApp(nil, wtFakeMappings())
	for _, c := range []struct{ lang, want string }{
		{"en", "No priced Watanya trips in September 2026"},
		{"ar", "لا توجد رحلات وطنية مسعّرة في الفترة سبتمبر 2026"},
	} {
		status, ctype, _, body := wtPost(t, app, map[string]string{
			"start_date": "2026-09-01", "end_date": "2026-09-30", "lang": c.lang})
		if status != 422 || !strings.HasPrefix(ctype, "application/json") {
			t.Fatalf("%s: status %d, %s", c.lang, status, ctype)
		}
		var got struct{ Error, Code string }
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got.Code != "WATANYA_EMPTY_RANGE" || got.Error != c.want {
			t.Errorf("%s: got %+v", c.lang, got)
		}
	}
}

func TestWatanyaExportFormats(t *testing.T) {
	trips, maps := wtFixture()
	app := wtApp(trips, maps)

	// Excel: the workbook, with the requested range — not the trip dates — as
	// the period (the fixture's trips all fall in September).
	status, _, _, body := wtPost(t, app, map[string]string{
		"start_date": "2026-08-20", "end_date": "2026-09-30"})
	if status != 200 || !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatalf("xlsx: status %d", status)
	}
	xl, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, sheet := range []string{"Summary", "Trip Details"} {
		a3, _ := xl.GetCellValue(sheet, "A3")
		if !strings.Contains(a3, "20 Aug – 30 Sep 2026") {
			t.Errorf("%s!A3 = %q, want the requested range", sheet, a3)
		}
	}
	xl.Close()

	for _, lang := range []string{"en", "ar"} {
		for _, part := range []string{"summary", "details"} {
			status, ctype, disp, body := wtPost(t, app, map[string]string{
				"start_date": "2026-09-01", "end_date": "2026-09-30", "lang": lang, "format": "pdf", "part": part})
			if status != 200 || ctype != "application/pdf" || !bytes.HasPrefix(body, []byte("%PDF-")) {
				t.Fatalf("%s/%s: status %d, %s", lang, part, status, ctype)
			}
			want := `attachment; filename="watanya_` + part + `_20260901_20260930.pdf"`
			if disp != want {
				t.Errorf("%s/%s: Content-Disposition %q, want %q", lang, part, disp, want)
			}
			if !bytes.Contains(body, []byte("/BaseFont /Cairo-Regular")) || !bytes.Contains(body, []byte("/FontFile2")) {
				t.Errorf("%s/%s: Cairo is not embedded", lang, part)
			}
		}
	}

	for _, bad := range []map[string]string{
		{"start_date": "2026-09-01", "end_date": "2026-09-30", "format": "pdf"},
		{"start_date": "2026-09-01", "end_date": "2026-09-30", "format": "pdf", "part": "all"},
		{"start_date": "2026-09-01", "end_date": "2026-09-30", "format": "docx"},
		{"start_date": "2026-09-01"},
	} {
		if status, _, _, _ := wtPost(t, app, bad); status != 400 {
			t.Errorf("%v: status %d, want 400", bad, status)
		}
	}
}
