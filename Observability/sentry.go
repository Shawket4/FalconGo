// Package Observability wires Sentry error reporting.
//
// Two rules govern this package.
//
// It is off unless SENTRY_DSN is set. With the variable unset or blank, Init
// installs nothing and returns a no-op flush, and the service behaves exactly
// as it did before Sentry existed. Nothing else in the codebase may assume a
// client is present.
//
// The scrubbing below is a COMPLIANCE CONTROL, not a preference. The operator
// publishes a privacy policy stating that error reports exclude personal data,
// and this is what makes that true. Falcon carries driver names, phone numbers,
// vehicle plates and GPS coordinates, all of which reach this process
// routinely. Do not relax beforeSend, and do not set SendDefaultPII to true,
// without a corresponding change to that policy.
package Observability

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

// Substrings that mark a key as carrying personal data. Matched against the key
// with case and separators removed, so driver_name, driverName and
// "DRIVER NAME" all match "name".
//
// Deliberately broad: over-redacting an error report costs a little debugging
// context, while under-redacting one puts a driver's phone number in a database
// we promised it would not be in.
var redactFragments = []string{
	"phone", "mobile", "email", "name", "address", "password", "passwd",
	"secret", "token", "authorization", "cookie", "session",
	"latitude", "longitude", "national", "ssn",
}

// Keys that are personal in full but too short to match as substrings — "lat"
// would otherwise match "plate" and "translate".
var redactExact = []string{"lat", "lng", "lon", "long", "coords", "gps"}

const redacted = "[redacted]"

func normalise(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsSensitive reports whether a field name carries personal data. Exported so
// the HTTP layer can use the same judgement the scrubber does.
func IsSensitive(key string) bool {
	k := normalise(key)
	for _, e := range redactExact {
		if k == e {
			return true
		}
	}
	for _, f := range redactFragments {
		if strings.Contains(k, f) {
			return true
		}
	}
	return false
}

// redactValue walks a decoded value, replacing the values of sensitive keys.
//
// Recursive on purpose: the interesting data is never at the top level. A trip
// payload nests the driver inside the trip inside the response, and a flat pass
// over the outermost map would miss every one of them.
func redactValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, inner := range t {
			if IsSensitive(k) {
				t[k] = redacted
			} else {
				t[k] = redactValue(inner)
			}
		}
		return t
	case []interface{}:
		for i, inner := range t {
			t[i] = redactValue(inner)
		}
		return t
	default:
		return v
	}
}

// redactQuery keeps the keys of a query string so a URL still says which
// filters were in play, and drops only the sensitive values.
func redactQuery(q string) string {
	parts := strings.Split(q, "&")
	for i, pair := range parts {
		if k, _, found := strings.Cut(pair, "="); found && IsSensitive(k) {
			parts[i] = k + "=" + redacted
		}
	}
	return strings.Join(parts, "&")
}

// beforeSend is the compliance control. See the package docs before changing it.
// ServiceName tags every event so one shared Sentry project can still be
// filtered per service. Without it you are left inferring the source from the
// release string or the SDK name.
const ServiceName = "falcon"

func beforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event.Tags == nil {
		event.Tags = map[string]string{}
	}
	event.Tags["service"] = ServiceName

	// Whatever the SDK inferred about the person making the request, drop it.
	// SendDefaultPII: false already suppresses most of this; clearing it
	// outright means a future SDK release cannot quietly widen what "default"
	// covers.
	event.User = sentry.User{}
	event.ServerName = ""

	if event.Request != nil {
		// The body is the highest-risk field in the whole event: on this
		// service it is trip, driver and fuel JSON.
		event.Request.Data = ""
		event.Request.Cookies = ""
		event.Request.QueryString = redactQuery(event.Request.QueryString)
		for name := range event.Request.Headers {
			if IsSensitive(name) {
				delete(event.Request.Headers, name)
			}
		}
		for name := range event.Request.Env {
			if IsSensitive(name) {
				event.Request.Env[name] = redacted
			}
		}
	}

	// sentry-go 0.49 has no Extra field: arbitrary data lives in Contexts,
	// which is walked below.

	for k := range event.Tags {
		if IsSensitive(k) {
			event.Tags[k] = redacted
		}
	}

	for name, ctx := range event.Contexts {
		for k, v := range ctx {
			if IsSensitive(k) {
				ctx[k] = redacted
			} else {
				ctx[k] = redactValue(v)
			}
		}
		event.Contexts[name] = ctx
	}

	for _, crumb := range event.Breadcrumbs {
		for k, v := range crumb.Data {
			if IsSensitive(k) {
				crumb.Data[k] = redacted
			} else {
				crumb.Data[k] = redactValue(v)
			}
		}
	}

	return event
}

// tracesSampleRate is the fraction of requests traced. Anything unparseable or
// out of range falls back to the default rather than failing a boot on a typo.
func tracesSampleRate() float64 {
	if v := strings.TrimSpace(os.Getenv("SENTRY_TRACES_SAMPLE_RATE")); v != "" {
		if r, err := strconv.ParseFloat(v, 64); err == nil && r >= 0 && r <= 1 {
			return r
		}
	}
	return 0.5
}

// Init installs Sentry, or doesn't.
//
// Returns a flush function that is always safe to call and to defer. When
// SENTRY_DSN is unset or blank no client is installed at all and the returned
// function does nothing.
func Init(release string) func() {
	dsn := strings.TrimSpace(os.Getenv("SENTRY_DSN"))
	if dsn == "" {
		return func() {}
	}

	environment := strings.TrimSpace(os.Getenv("SENTRY_ENVIRONMENT"))
	if environment == "" {
		environment = "production"
	}
	if r := strings.TrimSpace(os.Getenv("SENTRY_RELEASE")); r != "" {
		release = r
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: environment,
		Release:     release,
		// Compliance: never send the SDK's idea of "default" personal data.
		SendDefaultPII: false,
		// Tracing on. Env-tunable so the rate can be dropped without a
		// rebuild — this Sentry runs on modest hardware and tracing is the
		// part that loads it.
		EnableTracing:    true,
		TracesSampleRate: tracesSampleRate(),
		BeforeSend:       beforeSend,
	})
	if err != nil {
		// A misconfigured DSN must not take the service down with it.
		return func() {}
	}

	return func() { sentry.Flush(2 * time.Second) }
}
