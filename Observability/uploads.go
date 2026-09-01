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
// The error's message carries the destination path, which ends in the
// uploader's original filename, and those are routinely a person's name. It
// goes through SanitizeMessage before it is attached — that is what makes the
// difference between "no such file or directory" (which is the diagnosis) and
// exporting somebody's name. Do not attach err.Error() raw.
func ReportFailedUpload(document string, err error) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("upload.document", document)
		detail := ""
		if err != nil {
			detail = ": " + SanitizeMessage(err.Error())
		}
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureMessage("failed to save uploaded document: " + document + detail)
	})
}
