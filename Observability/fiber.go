package Observability

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/contrib/fibersentry"
	"github.com/gofiber/fiber/v2"
)

// Middleware attaches per-request Sentry scope and reports panics.
//
// Safe to install unconditionally: with no client configured the SDK's hub is
// a no-op, so this costs a map lookup and nothing else.
func Middleware() fiber.Handler {
	return fibersentry.New(fibersentry.Config{
		// Re-panic so the outermost recover (FiberConfig) still produces the
		// 500 the client expects; Sentry only observes. Handler panics never
		// get this far: the inner recover turns them into errors first and
		// reports them through ReportPanic.
		Repanic: true,
		// Wait for delivery before the handler returns, so an error that kills
		// the process still reaches Sentry.
		WaitForDelivery: false,
	})
}

// captureMessage reports on the request's own hub, with the tags applied to
// that hub's scope.
//
// This is not a stylistic wrapper. sentry.WithScope pushes a scope onto the
// CURRENT hub, which is the global one; fibersentry gives each request its own
// hub off the context. Configuring the global scope and then capturing on the
// request hub sets tags on one scope and reports through another, and the tags
// are silently dropped — the event still arrives, which is why it looks like it
// works. Both halves must be the same hub.
func captureMessage(hub *sentry.Hub, message string, configure func(*sentry.Scope)) {
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.WithScope(func(scope *sentry.Scope) {
		configure(scope)
		hub.CaptureMessage(message)
	})
}

// CaptureServerErrors reports any response that leaves with a 5xx status.
//
// The ErrorHandler below only sees handlers that RETURN an error. This service
// mostly does not: 264 call sites answer with c.Status(500).JSON(...) and
// return nil, so Fiber's error path is never entered and every one of those
// faults was invisible. This middleware reads the status after the handler has
// run, which catches both styles.
//
// Deliberately status-based rather than error-based: by this point there may be
// no error value left to report, so the event carries the route and status. The
// detail stays in the service log, because a response body is not something we
// can key-redact and must never be attached.
func CaptureServerErrors() fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := c.Next()

		// Unrouted 404s are worth knowing about; "record not found" 404s are
		// not. A request that matched no route at all means a client is asking
		// for something this service does not serve — a stale frontend build,
		// a typo'd path, a renamed endpoint nobody updated. A 404 from a
		// handler that looked something up and found nothing is ordinary.
		//
		// c.Route().Path is "/" for unmatched requests, which is how the two
		// are told apart without touching every handler.
		//
		// This is checked BEFORE handing the error on, because Fiber reports an
		// unmatched route by RETURNING fiber.ErrNotFound from c.Next() rather
		// than by setting a status. Behind the early return below, this branch
		// was unreachable and no unrouted 404 was ever reported.
		status := c.Response().StatusCode()
		var fe *fiber.Error
		if errors.As(err, &fe) {
			status = fe.Code
		}

		if status == fiber.StatusNotFound && c.Route().Path == "/" && c.Path() != "/" {
			captureMessage(
				fibersentry.GetHubFromContext(c),
				"no route for "+c.Method()+" "+c.Path(),
				func(scope *sentry.Scope) {
					scope.SetTag("http.status", "404")
					scope.SetTag("http.method", c.Method())
					// The path, not the route: there is no route, and the path
					// is the whole point of the report.
					scope.SetTag("http.path", c.Path())
					scope.SetLevel(sentry.LevelWarning)
				},
			)
			return err
		}

		// A handler that returned an error is reported by ErrorHandler, which
		// has the error itself and is strictly more useful. Don't double-report.
		if err != nil {
			return err
		}

		if status >= fiber.StatusInternalServerError {
			captureMessage(
				fibersentry.GetHubFromContext(c),
				c.Method()+" "+c.Route().Path+" responded "+strconv.Itoa(status),
				func(scope *sentry.Scope) {
					scope.SetTag("http.status", strconv.Itoa(status))
					scope.SetTag("http.route", c.Route().Path)
					scope.SetTag("http.method", c.Method())
					scope.SetLevel(sentry.LevelError)
				},
			)
		}
		return nil
	}
}

// panicReportedKey marks a request whose panic ReportPanic already sent, so
// ErrorHandler does not report the same fault a second time as a plain error.
const panicReportedKey = "observability.panic_reported"

// ReportPanic is the StackTraceHandler for Fiber's recover middleware: it
// writes the stack to the service log and reports the panic on the request's
// own hub, with the stack trace of the panicking goroutine.
//
// recover sits INSIDE the AI audit middleware (so a panicking write is still
// audited, with status 500), which puts it inside the Sentry middleware too:
// the panic is turned into an error before fibersentry could see it. This is
// what keeps it reported as a panic rather than only as a "500".
func ReportPanic(c *fiber.Ctx, e interface{}) {
	_, _ = os.Stderr.WriteString(fmt.Sprintf("panic: %v\n%s\n", e, debug.Stack()))

	hub := fibersentry.GetHubFromContext(c)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.WithScope(func(scope *sentry.Scope) {
		// Copies: the event outlives the request, and fiber reuses these
		// buffers for the next one.
		scope.SetTag("http.route", strings.Clone(c.Route().Path))
		scope.SetTag("http.method", strings.Clone(c.Method()))
		scope.SetLevel(sentry.LevelFatal)
		hub.RecoverWithContext(c.UserContext(), e)
	})
	c.Locals(panicReportedKey, true)
}

// LogPanic is the StackTraceHandler for the OUTERMOST recover, the safety net
// for a panic in the global middleware itself (which sits outside the inner
// recover). The Sentry middleware inside it has already reported that panic
// and re-panicked, so this only logs the stack and marks it reported.
func LogPanic(c *fiber.Ctx, e interface{}) {
	_, _ = os.Stderr.WriteString(fmt.Sprintf("panic: %v\n%s\n", e, debug.Stack()))
	c.Locals(panicReportedKey, true)
}

// ErrorHandler reports server faults and hands everything else to Fiber.
//
// 5xx only, deliberately. A client sending a malformed body or hitting a route
// it is not authorised for is not a bug in this service; reporting 4xx would
// bury the events that are with an unbounded stream of ones that are not.
//
// Wraps `next`, which should be the handler Fiber would otherwise have used.
func ErrorHandler(next fiber.ErrorHandler) fiber.ErrorHandler {
	if next == nil {
		next = fiber.DefaultErrorHandler
	}
	return func(c *fiber.Ctx, err error) error {
		status := fiber.StatusInternalServerError
		var fe *fiber.Error
		if errors.As(err, &fe) {
			status = fe.Code
		}

		reported, _ := c.Locals(panicReportedKey).(bool)
		if status >= fiber.StatusInternalServerError && !reported {
			// Use the request's own hub so the event carries this request's
			// scope and nothing from a concurrent one.
			if hub := fibersentry.GetHubFromContext(c); hub != nil {
				hub.CaptureException(err)
			} else {
				sentry.CaptureException(err)
			}
		}

		return next(c, err)
	}
}

// Span wraps an expensive step so it appears as a child span on the request's
// transaction — an export that spends its time in SQL rather than in xlsx
// generation is otherwise indistinguishable from one that does the reverse.
//
// No-op when Sentry is not configured, and no-op when the request has no
// transaction (sampled out), so it is safe to sprinkle on hot paths.
func Span(c *fiber.Ctx, operation string, fn func() error) error {
	hub := fibersentry.GetHubFromContext(c)
	if hub == nil {
		return fn()
	}
	// UserContext, not Context: ContinueTrace puts the transaction on the
	// former, and a span started from the fasthttp context has no parent and
	// silently never appears on the trace.
	span := sentry.StartSpan(c.UserContext(), operation)
	if span == nil {
		return fn()
	}
	defer span.Finish()

	err := fn()
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	}
	return err
}
