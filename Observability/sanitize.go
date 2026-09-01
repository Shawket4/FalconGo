package Observability

import (
	"regexp"
	"strings"
)

// SanitizeMessage makes a free-text error message safe to export.
//
// The scrubber redacts by KEY, walking structured data. It cannot help with a
// message, which is why error text was previously withheld from Sentry
// altogether — safe, but it left the events thin, with a job name and no
// indication of what actually went wrong.
//
// This redacts inside the text instead, so the message can be attached in full.
// It targets the shapes personal data actually takes in an error string: a
// key=value pair in a URL's query, a JSON field, a "key: value" fragment, an
// email address, a phone number, a bearer token. The key judgement is
// IsSensitive, the same one the scrubber uses, so the two cannot drift.
//
// Compliance control. It is deliberately conservative: over-redacting costs a
// little debugging detail, under-redacting exports somebody's phone number.
func SanitizeMessage(s string) string {
	if s == "" {
		return s
	}
	s = queryPairRe.ReplaceAllStringFunc(s, redactPairIfSensitive)
	s = jsonPairRe.ReplaceAllStringFunc(s, redactPairIfSensitive)
	s = colonPairRe.ReplaceAllStringFunc(s, redactPairIfSensitive)
	s = emailRe.ReplaceAllString(s, redacted)
	s = jwtRe.ReplaceAllString(s, redacted)
	s = intlPhoneRe.ReplaceAllString(s, redacted)
	s = localMobileRe.ReplaceAllString(s, redacted)

	// A message long enough to be a pasted payload is not a message.
	const maxLen = 2000
	if len(s) > maxLen {
		s = s[:maxLen] + "…[truncated]"
	}
	return s
}

var (
	// key=value, as in a URL query or a form body.
	queryPairRe = regexp.MustCompile(`([A-Za-z_][\w\-.]*)=([^&\s"'<>]+)`)
	// "key":"value", as in JSON.
	jsonPairRe = regexp.MustCompile(`"([^"]{1,64})"\s*:\s*"([^"]*)"`)
	// key: value, as in a header dump or a Go struct print.
	colonPairRe = regexp.MustCompile(`\b([A-Za-z_][\w\-.]*)\s*:\s+([^\s,;}]+)`)

	emailRe = regexp.MustCompile(`[\w.+\-]+@[\w\-]+\.[\w.\-]+`)
	jwtRe   = regexp.MustCompile(`eyJ[\w\-]{4,}\.[\w\-]{4,}(\.[\w\-]+)?`)
	// +20… and other international forms.
	intlPhoneRe = regexp.MustCompile(`\+\d[\d\s\-]{8,16}\d`)
	// Egyptian mobiles as written locally: 01x + 8 digits.
	localMobileRe = regexp.MustCompile(`\b01\d{9}\b`)

	// Splits a matched pair back into its key so IsSensitive can judge it.
	pairKeyRe = regexp.MustCompile(`^"?([A-Za-z_][\w\-.]*)"?\s*[:=]`)
)

// redactPairIfSensitive keeps the key and replaces the value, so the message
// still says WHICH field was involved — that is usually the useful half.
func redactPairIfSensitive(match string) string {
	km := pairKeyRe.FindStringSubmatch(match)
	if km == nil || !IsSensitive(km[1]) {
		return match
	}
	sep := strings.IndexAny(match, ":=")
	if sep < 0 {
		return match
	}
	return match[:sep+1] + redacted
}
