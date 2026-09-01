package Observability

import "github.com/getsentry/sentry-go"

// ReportFailedUpload reports a document that could not be written to disk.
//
// These failures were logged and stepped over: the surrounding code is careful
// not to point the record at a file that is not there, so nothing dangles, but
// the caller is still answered 200 and believes the licence was filed. A full
// disk or a missing uploads directory is therefore silent until somebody goes
// looking for a document that was never stored.
//
// The document TYPE is reported and nothing else. The error's message carries
// the destination path, which ends in the uploader's original filename, and
// those are routinely a person's name. The scrubber redacts by key and cannot
// clean a message, so the message must not be attached — compliance control,
// do not add the error here.
func ReportFailedUpload(document string) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("upload.document", document)
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureMessage("failed to save uploaded document: " + document)
	})
}
