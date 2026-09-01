package Observability

import (
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
	// The error's message must not ride along: these carry upstream URLs.
	if got := e.Message; got != "background job failed: vehicle_scraper at login" {
		t.Errorf("message carried more than the shape: %q", got)
	}
}

type errNamed struct{}

func (errNamed) Error() string {
	return "Get \"https://fms-gps.etit-eg.com/login?user=admin&pass=hunter2\": timeout"
}
