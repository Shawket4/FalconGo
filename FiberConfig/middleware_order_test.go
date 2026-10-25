package FiberConfig

import (
	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/middleware"
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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

// auditCapturingDB is a gorm handle that never connects anywhere and
// hands every ai_audit_logs row AIAudit creates to the test instead.
func auditCapturingDB(t *testing.T) (*gorm.DB, func() []Models.AiAuditLog) {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=none"}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true, // BEGIN would connect too
	})
	if err != nil {
		t.Fatalf("dry-run db: %v", err)
	}
	var mu sync.Mutex
	var rows []Models.AiAuditLog
	// Replace, not Before: the real create would still try to connect.
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Models.AiAuditLog); ok {
			mu.Lock()
			rows = append(rows, *row)
			mu.Unlock()
		}
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	return db, func() []Models.AiAuditLog {
		mu.Lock()
		defer mu.Unlock()
		return append([]Models.AiAuditLog(nil), rows...)
	}
}

// A handler panic must come back as a 500 rather than killing the process,
// still be audited by AIAudit (recover sits inside it), and still reach Sentry
// exactly once (as the panic, not again as a generic error).
func TestHandlerPanicIsA500AuditedAndReportedOnce(t *testing.T) {
	tr := &capturingTransport{}
	if err := sentry.Init(sentry.ClientOptions{Dsn: "https://key@example.invalid/1", Transport: tr}); err != nil {
		t.Fatalf("sentry init: %v", err)
	}
	defer sentry.Init(sentry.ClientOptions{})

	db, audited := auditCapturingDB(t)
	prevDB := Models.DB
	Models.DB = db
	defer func() { Models.DB = prevDB }()

	app := fiber.New(fiber.Config{ErrorHandler: Observability.ErrorHandler(nil)})
	useGlobalMiddleware(app)
	app.Post("/api/boom", func(c *fiber.Ctx) error {
		var u *Models.User
		_ = u.Name // the nil dereference every CurrentUser handler used to do
		return nil
	})
	app.Get("/api/still-up", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("POST", "/api/boom", strings.NewReader(`{"a":1}`))
	req.Header.Set(middleware.HeaderViaAI, "advisor")
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("panicking request: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("panicking handler = %d; want 500", resp.StatusCode)
	}

	// The process (here: the app) survives and serves the next request.
	resp, err = app.Test(httptest.NewRequest("GET", "/api/still-up", nil), -1)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("request after a panic = %v, %v; want 200", resp, err)
	}

	rows := audited()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d; want 1 (recover must sit inside AIAudit)", len(rows))
	}
	if rows[0].StatusCode != fiber.StatusInternalServerError || rows[0].Path != "/api/boom" {
		t.Errorf("audit row = %d %s; want 500 /api/boom", rows[0].StatusCode, rows[0].Path)
	}

	sentry.Flush(2 * time.Second)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.events) != 1 {
		for _, e := range tr.events {
			t.Logf("event: level=%s message=%q exceptions=%d tags=%v", e.Level, e.Message, len(e.Exception), e.Tags)
		}
		t.Fatalf("sentry events = %d; want exactly 1", len(tr.events))
	}
	e := tr.events[0]
	if e.Level != sentry.LevelFatal || len(e.Exception) == 0 {
		t.Errorf("event level=%q exceptions=%d; want the panic, reported fatal", e.Level, len(e.Exception))
	}
	// Tags are copies: fiber reuses the request's buffers once it is done,
	// and the event is sent after that (this read "GETT" before the clone).
	if e.Tags["http.method"] != "POST" || e.Tags["http.route"] != "/api/boom" {
		t.Errorf("event tags = %v; want POST /api/boom", e.Tags)
	}
}

// A panic outside the inner recover (in the global middleware itself) gets
// past it: the Sentry middleware reports it and re-panics, and the outermost
// recover turns it into a 500 without reporting it a second time. Built from
// the same two pieces useGlobalMiddleware installs first.
func TestPanicOutsideTheInnerRecoverIsA500ReportedOnce(t *testing.T) {
	tr := &capturingTransport{}
	if err := sentry.Init(sentry.ClientOptions{Dsn: "https://key@example.invalid/1", Transport: tr}); err != nil {
		t.Fatalf("sentry init: %v", err)
	}
	defer sentry.Init(sentry.ClientOptions{})

	app := fiber.New(fiber.Config{ErrorHandler: Observability.ErrorHandler(nil)})
	app.Use(recover.New(recover.Config{EnableStackTrace: true, StackTraceHandler: Observability.LogPanic}))
	app.Use(Observability.Middleware())
	app.Get("/x", func(c *fiber.Ctx) error { panic("middleware fault") })

	resp, err := app.Test(httptest.NewRequest("GET", "/x", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", resp.StatusCode)
	}
	sentry.Flush(2 * time.Second)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.events) != 1 {
		t.Fatalf("sentry events = %d; want exactly 1", len(tr.events))
	}
}
