package Observability

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v2"
)

// The frontend's trace id must survive onto this service's events, or the two
// ends are separate issues that merely share a timestamp.
const (
	callerTraceID = "d49d9bf66f13450b81f65bc51cf49c03"
	callerSpanID  = "a1b2c3d4e5f60718"
)

func runTraced(t *testing.T, register func(*fiber.App), header string) []*sentry.Event {
	t.Helper()
	tr := &capturingTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              "https://key@example.invalid/1",
		Transport:        tr,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer sentry.Init(sentry.ClientOptions{})

	app := fiber.New()
	app.Use(Middleware())
	app.Use(CaptureServerErrors())
	app.Use(ContinueTrace())
	register(app)

	req := httptest.NewRequest("GET", "/api/boom", nil)
	if header != "" {
		req.Header.Set("sentry-trace", header)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	sentry.Flush(2 * time.Second)

	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.events
}

func boom(app *fiber.App) {
	app.Get("/api/boom", func(c *fiber.Ctx) error {
		return c.Status(500).JSON(fiber.Map{"error": "nope"})
	})
}

func traceIDOf(e *sentry.Event) string {
	if tc, ok := e.Contexts["trace"]; ok {
		if id, ok := tc["trace_id"]; ok {
			return toString(id)
		}
	}
	return ""
}

func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case sentry.TraceID:
		return s.String()
	default:
		return ""
	}
}

// The regression this file exists for. fibersentry ignores sentry-trace, so
// before ContinueTrace every event opened a new trace and nothing tied the
// frontend error to the backend one that caused it.
func TestIncomingTraceIsContinued(t *testing.T) {
	events := runTraced(t, boom, callerTraceID+"-"+callerSpanID+"-1")

	var errEvents []*sentry.Event
	for _, e := range events {
		if e.Type != "transaction" {
			errEvents = append(errEvents, e)
		}
	}
	if len(errEvents) != 1 {
		t.Fatalf("want 1 error event, got %d of %d total", len(errEvents), len(events))
	}
	if got := traceIDOf(errEvents[0]); got != callerTraceID {
		t.Errorf("trace_id = %q, want the caller's %q", got, callerTraceID)
	}
}

// Without the header we must still produce a trace, just our own one.
func TestAbsentTraceHeaderStillGetsATrace(t *testing.T) {
	events := runTraced(t, boom, "")
	for _, e := range events {
		if e.Type == "transaction" {
			continue
		}
		if got := traceIDOf(e); got == "" {
			t.Error("error event carries no trace at all")
		} else if got == callerTraceID {
			t.Errorf("invented the caller's trace id from nowhere: %q", got)
		}
	}
}
