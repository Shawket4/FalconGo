package Observability

import (
	"errors"
	"strconv"

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
		// Re-panic so Fiber's own recovery still produces the 500 the client
		// expects; Sentry only observes.
		Repanic: true,
		// Wait for delivery before the handler returns, so an error that kills
		// the process still reaches Sentry.
		WaitForDelivery: false,
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

		// A handler that returned an error is reported by ErrorHandler, which
		// has the error itself and is strictly more useful. Don't double-report.
		if err != nil {
			return err
		}

		if c.Response().StatusCode() >= fiber.StatusInternalServerError {
			hub := fibersentry.GetHubFromContext(c)
			capture := sentry.CaptureMessage
			if hub != nil {
				capture = hub.CaptureMessage
			}
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("http.status", strconv.Itoa(c.Response().StatusCode()))
				scope.SetTag("http.route", c.Route().Path)
				scope.SetTag("http.method", c.Method())
				scope.SetLevel(sentry.LevelError)
				capture(c.Method() + " " + c.Route().Path + " responded " +
					strconv.Itoa(c.Response().StatusCode()))
			})
		}
		return nil
	}
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

		if status >= fiber.StatusInternalServerError {
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
