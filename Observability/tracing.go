package Observability

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/contrib/fibersentry"
	"github.com/gofiber/fiber/v2"
)

// ContinueTrace joins the trace the caller started, instead of beginning a new
// one.
//
// The browser mints a trace id per interaction and sends it on as `sentry-trace`
// and `baggage`. fibersentry does not look at either header — it clones a hub
// and sets the request on the scope, and that is all — so every event this
// service reported opened a brand new trace. A frontend error and the backend
// error that caused it were two unrelated issues that happened to share a
// timestamp, and correlating them meant reading clocks.
//
// sentry.ContinueTrace does two things, and the second is the one that matters
// most here: it starts the transaction as a child of the caller's span, and it
// puts the incoming trace on the SCOPE. Scope propagation applies to error
// events too, so even when the transaction is sampled away — and at a 0.5 rate
// half of them are — the error still carries the frontend's trace id and the
// two ends still line up in Sentry.
//
// Install after Middleware(), which is what puts the hub on the context.
func ContinueTrace() fiber.Handler {
	return func(c *fiber.Ctx) error {
		hub := fibersentry.GetHubFromContext(c)
		if hub == nil {
			return c.Next()
		}

		// Route is matched before middleware runs, so this is the low-cardinality
		// path ("/api/trips/:id") rather than the filled-in one. A transaction
		// named with the id would be its own group, one per trip, forever.
		name := c.Method() + " " + c.Route().Path

		transaction := sentry.StartTransaction(
			sentry.SetHubOnContext(c.UserContext(), hub),
			name,
			sentry.ContinueTrace(hub, c.Get(sentry.SentryTraceHeader), c.Get(sentry.SentryBaggageHeader)),
			sentry.WithOpName("http.server"),
			sentry.WithTransactionSource(sentry.SourceRoute),
		)

		// Hand the transaction's context down so Span() can hang children off it.
		c.SetUserContext(transaction.Context())

		err := c.Next()

		status := c.Response().StatusCode()
		var fe *fiber.Error
		if err != nil {
			status = http.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				fe = e
				status = fe.Code
			}
		}
		transaction.Status = sentry.HTTPtoSpanStatus(status)
		transaction.SetData("http.response.status_code", status)
		transaction.Finish()

		return err
	}
}

// PropagateTrace copies the current trace onto an outgoing request.
//
// A hop between our own services is still a hop: without these headers
// etit-proxy opens a fresh trace for a call Falcon made on a user's behalf, and
// the two halves of the same operation land in Sentry unconnected. Same
// headers the browser sends us.
//
// No-op when the context carries no span, so it is safe on background work.
func PropagateTrace(ctx context.Context, req *http.Request) {
	if ctx == nil || req == nil {
		return
	}
	span := sentry.SpanFromContext(ctx)
	if span == nil {
		return
	}
	req.Header.Set(sentry.SentryTraceHeader, span.ToSentryTrace())
	if b := span.ToBaggage(); b != "" {
		req.Header.Set(sentry.SentryBaggageHeader, b)
	}
}
