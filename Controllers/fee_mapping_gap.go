package Controllers

import (
	"github.com/getsentry/sentry-go"
)

// unregisteredDropOff is the placeholder a container carries before anyone has
// said where it went. It is not a real place, so it is never priced and never
// reported as a missing mapping.
//
// Named because it was written as a bare literal in two places and only one of
// them guarded against it, which is how the same request could be rejected by
// one code path and tolerated by the next.
const unregisteredDropOff = "غير مسجل"

// reportMissingFeeMapping records a priced route that does not exist.
//
// This surfaces to the user as a 4xx, and 4xx is normally NOT reported — a
// client sending a bad request is not a bug in this service. This one is the
// exception, and deliberately so: nobody typed it. The company, terminal and
// drop-off all come from rows we already hold, so a missing mapping means our
// own pricing data has a hole, and the user is simply the first to walk into
// it. Those are worth seeing accumulate; they tell you which routes to price.
//
// No PII: a company, a terminal and a drop-off are places, not people.
func reportMissingFeeMapping(company, terminal, dropOff string) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("fee_mapping.company", company)
		scope.SetTag("fee_mapping.terminal", terminal)
		scope.SetTag("fee_mapping.drop_off", dropOff)
		scope.SetLevel(sentry.LevelWarning)
		sentry.CaptureMessage("missing fee mapping: " + company + " / " + terminal + " -> " + dropOff)
	})
}
