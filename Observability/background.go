package Observability

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
)

// ReportJobFailure reports a background job that gave up before doing its work.
//
// These jobs answer to nobody: no request is in flight, so the HTTP middleware
// never sees them, and a job that logs and returns looks exactly like a job
// that had nothing to do. The vehicle scraper failing to log in is live
// tracking quietly going stale until somebody notices the map is old.
//
// The job and the stage it stopped at are the actionable part, and the error's
// TYPE is attached rather than its message: these errors come back from HTTP
// clients and carry request URLs, which for the upstreams here include
// identifiers and sometimes credentials in the query string. The scrubber
// redacts by key and cannot clean a message, so the message stays in the
// service log where it is already written. Compliance control.
func ReportJobFailure(job, stage string, err error) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("job.name", job)
		scope.SetTag("job.stage", stage)
		if err != nil {
			scope.SetTag("job.error_type", fmt.Sprintf("%T", err))
		}
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureMessage("background job failed: " + job + " at " + stage)
	})
}

// Guard reports a panic in a background goroutine and stops it taking the
// process down. Defer it at the top of the goroutine's body.
//
// Go kills the process on an unrecovered panic in any goroutine, and the
// fibersentry middleware only wraps request handlers — so a panic in a worker
// took the API server with it and reported nothing on the way out. The restart
// was the only evidence. Reporting and returning is strictly better: the tick
// that panicked is lost, the next one runs, and the API keeps serving.
func Guard(job string) {
	r := recover()
	if r == nil {
		return
	}
	hub := sentry.CurrentHub().Clone()
	hub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("job.name", job)
		scope.SetLevel(sentry.LevelFatal)
	})
	hub.Recover(r)
	hub.Flush(2 * time.Second)
	log.Printf("recovered panic in background job %s: %v", job, r)
}

// ReportBatchFailure reports a background round that failed wholesale.
//
// Per-item failures in a batch are ordinary and belong in the log; reporting
// each one floods Sentry and teaches everyone to scroll past it. What matters
// is a round where nothing got through, which means the job has stopped doing
// its work. One event per round, counts only — never the identifiers of the
// items that failed, which here are trips and so are drivers.
func ReportBatchFailure(job string, failed, total int) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("job.name", job)
		scope.SetTag("job.failed", strconv.Itoa(failed))
		scope.SetTag("job.total", strconv.Itoa(total))
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureMessage(fmt.Sprintf("background job %s: %d of %d failed", job, failed, total))
	})
}
