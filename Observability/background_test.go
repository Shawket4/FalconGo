package Observability

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

func withCapture(t *testing.T, fn func()) []*sentry.Event {
	t.Helper()
	tr := &capturingTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@example.invalid/1",
		Transport: tr,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer sentry.Init(sentry.ClientOptions{})
	fn()
	sentry.Flush(2 * time.Second)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.events
}

// The behaviour change this helper exists for: Go takes the whole process down
// on an unrecovered panic in any goroutine, and nothing reported it on the way
// out. If this test ever fails by crashing the test binary, the guard is gone.
func TestGuardReportsAPanicAndLetsTheProcessLive(t *testing.T) {
	var wg sync.WaitGroup
	events := withCapture(t, func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer Guard("a_worker")
			panic("worker exploded")
		}()
		wg.Wait()
	})

	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Tags["job.name"] != "a_worker" {
		t.Errorf("job.name = %q, want a_worker", e.Tags["job.name"])
	}
	if e.Level != sentry.LevelFatal {
		t.Errorf("level = %v, want fatal", e.Level)
	}
}

// A goroutine that returns normally must not report anything.
func TestGuardIsSilentWithoutAPanic(t *testing.T) {
	var wg sync.WaitGroup
	events := withCapture(t, func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer Guard("a_worker")
		}()
		wg.Wait()
	})
	if len(events) != 0 {
		t.Fatalf("a clean run raised %d event(s)", len(events))
	}
}

func TestJobFailureCarriesTheJobAndStage(t *testing.T) {
	events := withCapture(t, func() {
		ReportJobFailure("vehicle_scraper", "login", errNamed{})
	})
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Tags["job.name"] != "vehicle_scraper" || e.Tags["job.stage"] != "login" {
		t.Errorf("tags = %v", e.Tags)
	}
	// The message now rides along — sanitized. The diagnosis must survive and
	// the credential must not.
	if !strings.Contains(e.Message, "timeout") {
		t.Errorf("lost the diagnosis: %q", e.Message)
	}
	if !strings.Contains(e.Message, "fms-gps.etit-eg.com") {
		t.Errorf("lost the host: %q", e.Message)
	}
	if strings.Contains(e.Message, "hunter2") {
		t.Errorf("exported a credential: %q", e.Message)
	}
}

type errNamed struct{}

func (errNamed) Error() string {
	return "Get \"https://fms-gps.etit-eg.com/login?user=admin&pass=hunter2\": timeout"
}
