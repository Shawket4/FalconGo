package Models

import "time"

// AiAuditLog records one write that reached the API through the AI advisor
// (a request carrying the X-Via-AI header). The advisor never writes to the
// database itself: it applies an approved suggestion by calling this API with
// the approving admin's own JWT, and this row is the trail of that call.
//
// UserID is always the verified JWT user, never anything the caller asserts in
// a header. 0 means the request carried X-Via-AI but never authenticated, so it
// was rejected before reaching a handler, which StatusCode shows.
type AiAuditLog struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	CreatedAt    time.Time `json:"created_at" gorm:"index"`
	UserID       uint      `json:"user_id" gorm:"not null;default:0;index"`
	UserName     string    `json:"user_name"`
	Permission   int       `json:"permission"`
	Via          string    `json:"via" gorm:"type:varchar(64)"`
	SuggestionID *string   `json:"suggestion_id" gorm:"type:varchar(128);index"`
	Method       string    `json:"method" gorm:"type:varchar(10)"`
	Path         string    `json:"path"`
	Query        string    `json:"query"`
	RequestBody  string    `json:"request_body" gorm:"type:text"`
	StatusCode   int       `json:"status_code"`
	DurationMs   int64     `json:"duration_ms"`
	IP           string    `json:"ip" gorm:"type:varchar(64)"`
}

func (AiAuditLog) TableName() string {
	return "ai_audit_logs"
}
