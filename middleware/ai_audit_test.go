package middleware

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
)

func TestAIWriteForbiddenOnlyRefusesLowPermissionAIWrites(t *testing.T) {
	cases := []struct {
		via, method string
		permission  int
		want        bool
	}{
		{"advisor", "PATCH", 3, true},
		{"advisor", "POST", 1, true},
		{"advisor", "DELETE", 0, true},
		{"advisor", "put", 3, true},
		{"advisor", "PATCH", 4, false},
		{"advisor", "POST", 5, false},
		{"advisor", "GET", 1, false},
		{"advisor", "HEAD", 1, false},
		{"advisor", "OPTIONS", 1, false},
		{"", "PATCH", 1, false},
		{"   ", "PATCH", 1, false},
	}
	for _, tc := range cases {
		if got := AIWriteForbidden(tc.via, tc.method, tc.permission); got != tc.want {
			t.Errorf("AIWriteForbidden(%q, %q, %d) = %v; want %v", tc.via, tc.method, tc.permission, got, tc.want)
		}
	}
}

func TestSanitizeAuditBodyRedactsSecretsAtAnyDepth(t *testing.T) {
	body := `{"operating_company":"watanya","password":"hunter2","nested":{"access_token":"abc","Client_Secret":"s"},` +
		`"items":[{"pin":"1234","fee_mapping_id":7}]}`
	got := SanitizeAuditBody(fiber.MIMEApplicationJSON, []byte(body))

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("sanitized body is not JSON: %v (%s)", err, got)
	}
	for _, secret := range []string{"hunter2", `"abc"`, `"s"`, "1234"} {
		if strings.Contains(got, secret) {
			t.Errorf("sanitized body still contains %s: %s", secret, got)
		}
	}
	if decoded["operating_company"] != "watanya" {
		t.Errorf("a plain field was changed: %s", got)
	}
	item := decoded["items"].([]interface{})[0].(map[string]interface{})
	if item["fee_mapping_id"] != float64(7) {
		t.Errorf("fee_mapping_id was redacted as if it were a pin: %s", got)
	}
}

func TestSanitizeAuditBodyFormAndOpaqueBodies(t *testing.T) {
	form := SanitizeAuditBody(fiber.MIMEApplicationForm, []byte("name=a&password=hunter2"))
	if strings.Contains(form, "hunter2") || !strings.Contains(form, "name=a") {
		t.Errorf("form body not redacted field by field: %s", form)
	}

	multipart := SanitizeAuditBody("multipart/form-data; boundary=x", []byte("--x\r\npassword=hunter2"))
	if strings.Contains(multipart, "hunter2") || !strings.Contains(multipart, "not stored") {
		t.Errorf("an unredactable body was stored: %s", multipart)
	}

	if got := SanitizeAuditBody(fiber.MIMEApplicationJSON, nil); got != "" {
		t.Errorf("empty body = %q; want empty", got)
	}
}

func TestSanitizeAuditBodyCapsLargeBodies(t *testing.T) {
	big := `{"note":"` + strings.Repeat("ج", 20*1024) + `"}`
	got := SanitizeAuditBody(fiber.MIMEApplicationJSON, []byte(big))
	if len(got) > aiAuditBodyCap {
		t.Errorf("sanitized body is %d bytes; cap is %d", len(got), aiAuditBodyCap)
	}
	if !utf8.ValidString(got) {
		t.Error("capping split a UTF-8 rune")
	}
	if !strings.HasSuffix(got, "[truncated]") {
		t.Error("a truncated body is not marked as truncated")
	}
}

func TestRedactQuery(t *testing.T) {
	got := redactQuery("limit=5&token=abc")
	if strings.Contains(got, "abc") || !strings.Contains(got, "limit=5") {
		t.Errorf("redactQuery = %q", got)
	}
	if redactQuery("") != "" {
		t.Error("an empty query should stay empty")
	}
}

// With no database the audit is skipped, and the response must be exactly the
// handler's: recording is best-effort and never fails or alters a request.
func TestAIAuditNeverAltersTheResponse(t *testing.T) {
	app := fiber.New()
	app.Use(AIAudit())
	app.Patch("/ok", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusAccepted).SendString("done")
	})
	app.Patch("/bad", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"message": "no"})
	})

	for path, want := range map[string]int{"/ok": fiber.StatusAccepted, "/bad": fiber.StatusBadRequest} {
		req := httptest.NewRequest("PATCH", path, strings.NewReader(`{"password":"x"}`))
		req.Header.Set(HeaderViaAI, "advisor")
		req.Header.Set(HeaderAISuggestionID, "6f1c2d1e-0000-4000-8000-000000000000")
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Errorf("%s through AIAudit = %d; want %d", path, resp.StatusCode, want)
		}
	}
}

func TestAuditStatusFollowsReturnedErrors(t *testing.T) {
	app := fiber.New()
	var got []int
	app.Use(func(c *fiber.Ctx) error {
		err := c.Next()
		got = append(got, auditStatus(c, err))
		return err
	})
	app.Post("/fiber-error", func(c *fiber.Ctx) error { return fiber.NewError(fiber.StatusConflict, "x") })
	app.Post("/plain-error", func(c *fiber.Ctx) error { return errors.New("boom") })
	app.Post("/ok", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusCreated) })

	for _, path := range []string{"/fiber-error", "/plain-error", "/ok"} {
		if _, err := app.Test(httptest.NewRequest("POST", path, nil)); err != nil {
			t.Fatal(err)
		}
	}
	want := []int{fiber.StatusConflict, fiber.StatusInternalServerError, fiber.StatusCreated}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("audited statuses = %v; want %v", got, want)
		}
	}
}
