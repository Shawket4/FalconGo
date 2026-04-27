package Models

type EmailConfig struct {
	SMTPServer   string
	SMTPPort     int
	Username     string
	Password     string
	FromEmail    string
	FromName     string
	TLSEnabled   bool
	SkipTLSCheck bool
}

// EmailMessage represents an email to be sent
type EmailMessage struct {
	To          []string
	CC          []string
	BCC         []string
	Subject     string
	Body        string
	IsHTML      bool
	Attachments []Attachment
}

// Attachment represents a file attachment
type Attachment struct {
	Filename string
	Data     []byte
	MimeType string
}
