package Observability

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v2"
)

// capturingTransport records what would have been sent, so the tests can assert
// on the event rather than on the code that builds it.
type capturingTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *capturingTransport) Configure(sentry.ClientOptions)        {}
func (t *capturingTransport) Flush(time.Duration) bool              { return true }
func (t *capturingTransport) Close()                                {}
func (t *capturingTransport) FlushWithContext(context.Context) bool { return true }
func (t *capturingTransport) SendEvent(e *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, e)
}

// run installs a capturing client, drives one request through the same
// middleware chain main.go uses, and returns the events it produced.
func run(t *testing.T, register func(*fiber.App), method, path string) []*sentry.Event {
	t.Helper()
	tr := &capturingTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@example.invalid/1",
		Transport: tr,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer sentry.Init(sentry.ClientOptions{})

	app := fiber.New()
	app.Use(Middleware())
	app.Use(CaptureServerErrors())
	register(app)

	resp, err := app.Test(httptest.NewRequest(method, path, nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	sentry.Flush(2 * time.Second)

	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.events
}

func tag(e *sentry.Event, k string) string { return e.Tags[k] }

// The regression this file exists for. The tags were being set on the global
// hub's scope and the event captured on the request's hub, so every one of
// these arrived untagged — and an untagged event still arrives, which is what
// made it invisible. Asserting on the event is the only way to see it.
func TestServerErrorsCarryTheirRouteAndStatus(t *testing.T) {
	events := run(t, func(app *fiber.App) {
		app.Get("/api/boom", func(c *fiber.Ctx) error {
			return c.Status(500).JSON(fiber.Map{"error": "nope"})
		})
	}, "GET", "/api/boom")

	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	e := events[0]
	if got := tag(e, "http.status"); got != "500" {
		t.Errorf("http.status = %q, want 500", got)
	}
	if got := tag(e, "http.route"); got != "/api/boom" {
		t.Errorf("http.route = %q, want /api/boom", got)
	}
	if got := tag(e, "http.method"); got != "GET" {
		t.Errorf("http.method = %q, want GET", got)
	}
	if e.Level != sentry.LevelError {
		t.Errorf("level = %v, want error", e.Level)
	}
}

// A response body is not something we can key-redact, so it must never ride
// along. This service answers 500s with JSON that has held plates and names.
func TestServerErrorEventsCarryNoResponseBody(t *testing.T) {
	events := run(t, func(app *fiber.App) {
		app.Get("/api/boom", func(c *fiber.Ctx) error {
			return c.Status(500).JSON(fiber.Map{"driver": "محمد علي", "plate": "ف د ى 6584"})
		})
	}, "GET", "/api/boom")

	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if msg := events[0].Message; msg != "GET /api/boom responded 500" {
		t.Errorf("message carried more than the shape: %q", msg)
	}
}

func TestUnroutedRequestsAreReportedWithTheirPath(t *testing.T) {
	events := run(t, func(app *fiber.App) {}, "GET", "/api/typo")

	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	e := events[0]
	if got := tag(e, "http.path"); got != "/api/typo" {
		t.Errorf("http.path = %q, want /api/typo", got)
	}
	if e.Level != sentry.LevelWarning {
		t.Errorf("level = %v, want warning", e.Level)
	}
}

// A handler that looked something up and found nothing is ordinary, and must
// not raise an issue — otherwise the unrouted 404s above are lost in the noise.
func TestHandled404sAreNotReported(t *testing.T) {
	events := run(t, func(app *fiber.App) {
		app.Get("/api/trips/:id", func(c *fiber.Ctx) error {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		})
	}, "GET", "/api/trips/999")

	if len(events) != 0 {
		t.Fatalf("a handled 404 raised %d event(s)", len(events))
	}
}

// The other half: ordinary success and client errors must stay silent.
func TestSuccessAndClientErrorsAreNotReported(t *testing.T) {
	for _, status := range []int{200, 400, 401, 403, 422} {
		events := run(t, func(app *fiber.App) {
			app.Get("/api/x", func(c *fiber.Ctx) error {
				return c.Status(status).JSON(fiber.Map{"ok": true})
			})
		}, "GET", "/api/x")
		if len(events) != 0 {
			t.Errorf("status %d raised %d event(s)", status, len(events))
		}
	}
}
