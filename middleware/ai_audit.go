package middleware

import (
	"Falcon/Models"
	"Falcon/Observability"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
)

// The AI advisor applies an approved suggestion by calling this API with the
// approving admin's own JWT and these two headers. Nothing it sends is trusted
// for identity: who made the change is always the verified JWT user.
const (
	HeaderViaAI          = "X-Via-AI"
	HeaderAISuggestionID = "X-AI-Suggestion-Id"

	// AIWritePermission is the floor for any write that arrives through the AI.
	AIWritePermission = 4

	// aiAuditBodyCap bounds the request body stored per audit row.
	aiAuditBodyCap = 16 * 1024
	// Header values land in bounded columns; longer ones are cut, not refused.
	aiAuditViaCap          = 64
	aiAuditSuggestionIDCap = 128

	redactedValue = "[REDACTED]"
)

// isWriteMethod reports whether the method can change state. GET, HEAD and
// OPTIONS are reads; everything else is treated as a write.
func isWriteMethod(method string) bool {
	switch strings.ToUpper(method) {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return false
	}
	return true
}

// AIWriteForbidden is the permission-floor decision for AI-originated
// requests: a write that carries X-Via-AI needs a user at AIWritePermission or
// above. Reads and requests not made through the AI are never refused here.
func AIWriteForbidden(via, method string, permission int) bool {
	return strings.TrimSpace(via) != "" && isWriteMethod(method) && permission < AIWritePermission
}

// isSensitiveKey matches field names whose values must never be stored.
func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, word := range []string{"password", "passwd", "token", "secret", "authorization", "api_key", "apikey"} {
		if strings.Contains(k, word) {
			return true
		}
	}
	// Driver PINs, matched as a whole word: a substring match would also take
	// "mapping" and "shipping".
	return k == "pin" || strings.HasPrefix(k, "pin_") || strings.HasSuffix(k, "_pin")
}

// redactJSON walks a decoded JSON value and replaces the value of every
// sensitive key, at any depth, with redactedValue.
func redactJSON(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = redactedValue
			} else {
				t[k] = redactJSON(val)
			}
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = redactJSON(val)
		}
		return t
	}
	return v
}

// redactQuery redacts sensitive parameters of a URL-encoded string. Input that
// does not parse is returned as a placeholder rather than stored raw.
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return fmt.Sprintf("[unparseable query, %d bytes]", len(raw))
	}
	for k := range values {
		if isSensitiveKey(k) {
			values[k] = []string{redactedValue}
		}
	}
	return values.Encode()
}

// capString cuts s to at most limit bytes without splitting a UTF-8 rune and
// marks the cut, so a truncated body is never mistaken for the whole one.
func capString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const marker = "…[truncated]"
	cut := limit - len(marker)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + marker
}

// SanitizeAuditBody renders a request body for the audit trail: sensitive
// fields redacted, at most aiAuditBodyCap bytes. JSON and URL-encoded forms
// are redacted field by field; any other body (multipart uploads, plain text)
// is recorded only by its size, since it cannot be redacted reliably.
func SanitizeAuditBody(contentType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	ct := strings.ToLower(contentType)

	var decoded interface{}
	if json.Unmarshal(body, &decoded) == nil {
		out, err := json.Marshal(redactJSON(decoded))
		if err != nil {
			return fmt.Sprintf("[unencodable JSON body, %d bytes]", len(body))
		}
		return capString(string(out), aiAuditBodyCap)
	}
	if strings.HasPrefix(ct, fiber.MIMEApplicationForm) {
		return capString(redactQuery(string(body)), aiAuditBodyCap)
	}
	return fmt.Sprintf("[%s body not stored, %d bytes]", strings.TrimSpace(strings.Split(ct, ";")[0]), len(body))
}

// auditStatus is the status the client will actually receive. A handler that
// returns an error has not set the status yet (the app ErrorHandler does that
// once the chain unwinds), so derive it from the error the same way.
func auditStatus(c *fiber.Ctx, err error) int {
	if err == nil {
		return c.Response().StatusCode()
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return fiber.StatusInternalServerError
}

// auditUser reads the verified user that Verify (pointer) or VerifyAny
// (value) stored in Locals. A request that never authenticated has none.
func auditUser(c *fiber.Ctx) (Models.User, bool) {
	switch u := c.Locals("user").(type) {
	case *Models.User:
		if u != nil {
			return *u, true
		}
	case Models.User:
		return u, true
	}
	return Models.User{}, false
}

// AIAudit records every write that arrives through the AI advisor in
// ai_audit_logs, failures included: the status code says whether the change
// was applied. Recording is best-effort and never changes the response.
func AIAudit() fiber.Handler {
	return func(c *fiber.Ctx) error {
		via := strings.TrimSpace(c.Get(HeaderViaAI))
		if via == "" || !isWriteMethod(c.Method()) {
			return c.Next()
		}

		start := time.Now()
		// Captured before the handler runs; fiber reuses these buffers once the
		// request is done, so everything kept is a copy.
		row := Models.AiAuditLog{
			Via:         capString(strings.Clone(via), aiAuditViaCap),
			Method:      strings.Clone(c.Method()),
			Path:        strings.Clone(c.Path()),
			Query:       redactQuery(string(c.Request().URI().QueryString())),
			RequestBody: SanitizeAuditBody(c.Get(fiber.HeaderContentType), c.Body()),
			IP:          strings.Clone(c.IP()),
		}
		if sid := strings.TrimSpace(c.Get(HeaderAISuggestionID)); sid != "" {
			s := capString(strings.Clone(sid), aiAuditSuggestionIDCap)
			row.SuggestionID = &s
		}

		err := c.Next()

		row.StatusCode = auditStatus(c, err)
		row.DurationMs = time.Since(start).Milliseconds()
		if user, ok := auditUser(c); ok {
			row.UserID = user.Id
			row.UserName = user.Name
			row.Permission = user.Permission
		}

		if Models.DB == nil {
			log.Printf("ai audit: no database, dropped %s %s", row.Method, row.Path)
			return err
		}
		if dbErr := Models.DB.Create(&row).Error; dbErr != nil {
			log.Printf("ai audit: recording %s %s: %v", row.Method, row.Path, dbErr)
			Observability.ReportJobFailure("ai_audit", "insert", dbErr)
		}
		return err
	}
}
