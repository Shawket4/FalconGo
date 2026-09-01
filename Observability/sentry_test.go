package Observability

import (
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestSensitiveKeysAreRecognisedInAnyShape(t *testing.T) {
	for _, key := range []string{
		"phone", "driver_name", "driverName", "DRIVER NAME", "e-mail",
		"Authorization", "password", "latitude", "national_id", "lat", "GPS",
	} {
		if !IsSensitive(key) {
			t.Errorf("%q should be redacted", key)
		}
	}
}

func TestOrdinaryKeysSurvive(t *testing.T) {
	// "plate" and "translate" contain "lat"; matching that as a substring
	// would redact half the schema.
	for _, key := range []string{
		"plate", "car_no_plate", "translate", "trip_id", "status", "count",
		"distance", "receipt_no", "template",
	} {
		if IsSensitive(key) {
			t.Errorf("%q should not be redacted", key)
		}
	}
}

func TestRedactionReachesNestedValues(t *testing.T) {
	v := map[string]interface{}{
		"trip": map[string]interface{}{
			"id": 7,
			"driver": map[string]interface{}{
				"name":  "Someone Real",
				"phone": "+201234567890",
			},
			"stops": []interface{}{
				map[string]interface{}{"latitude": 30.1, "longitude": 31.2, "label": "depot"},
			},
		},
	}
	redactValue(v)

	trip := v["trip"].(map[string]interface{})
	if trip["id"] != 7 {
		t.Error("non-sensitive values must be left alone")
	}
	// A sensitive key takes its whole subtree with it, rather than being walked
	// into and cleaned field by field. Costs the ids underneath; means a
	// personal value under an innocent-looking key cannot survive inside a
	// container we already know is personal.
	if trip["driver"] != redacted {
		t.Errorf("the driver subtree was not redacted: %v", trip["driver"])
	}
	stop := trip["stops"].([]interface{})[0].(map[string]interface{})
	if stop["latitude"] != redacted || stop["longitude"] != redacted {
		t.Errorf("coordinates inside an array were not redacted: %v", stop)
	}
	if stop["label"] != "depot" {
		t.Error("ordinary fields must survive")
	}
}

func TestQueryStringsKeepTheirKeys(t *testing.T) {
	got := redactQuery("month=2025-05&phone=%2B20123&company=Watanya")
	want := "month=2025-05&phone=" + redacted + "&company=Watanya"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestBeforeSendDropsBodyCookiesAndAuthHeaders(t *testing.T) {
	event := &sentry.Event{
		User:       sentry.User{Email: "driver@example.com"},
		ServerName: "vps-1",
		Request: &sentry.Request{
			Data:        `{"driver_name":"Someone Real"}`,
			Cookies:     "session=abc",
			QueryString: "lat=30.1&month=2025-05",
			Headers: map[string]string{
				"Authorization": "Bearer real-token",
				"User-Agent":    "curl/8",
			},
		},
	}

	out := beforeSend(event, nil)

	if out.Request.Data != "" {
		t.Error("the request body must never be sent")
	}
	if out.Request.Cookies != "" {
		t.Error("cookies must never be sent")
	}
	if _, present := out.Request.Headers["Authorization"]; present {
		t.Error("the auth header must never be sent")
	}
	if out.Request.Headers["User-Agent"] != "curl/8" {
		t.Error("ordinary headers should survive for triage")
	}
	if out.Request.QueryString != "lat="+redacted+"&month=2025-05" {
		t.Errorf("query string not redacted: %q", out.Request.QueryString)
	}
	if out.User.Email != "" {
		t.Error("user identity must be cleared")
	}
	if out.ServerName != "" {
		t.Error("server name must be cleared")
	}
}

// With no DSN the package must install nothing and hand back a flush that is
// safe to defer. This is the switch the whole rollout depends on.
func TestInitIsInertWithoutADsn(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")
	flush := Init("test@0")
	if flush == nil {
		t.Fatal("Init must always return a callable flush")
	}
	flush()
}

// APEX-7 arrived with job.name redacted, so the event could not say which job
// had failed. The scrubber had eaten the tag that made it actionable.
func TestMachineMetadataKeysSurvive(t *testing.T) {
	for _, key := range []string{
		"job.name", "os.name", "runtime.name", "browser.name", "sdk.name", "service.name",
	} {
		if IsSensitive(key) {
			t.Errorf("%s should not be redacted — it names a machine, not a person", key)
		}
	}
}

// And the half that must keep working: these name people.
func TestPersonNameKeysAreStillRedacted(t *testing.T) {
	for _, key := range []string{
		"driver_name", "first_name", "full_name", "DriverName", "user_name",
		"device.name", "filename", "customer",
	} {
		if !IsSensitive(key) {
			t.Errorf("%s must be redacted", key)
		}
	}
}
