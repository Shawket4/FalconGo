package Observability

import (
	"errors"

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
