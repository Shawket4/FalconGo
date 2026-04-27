package Constants

import (
	"Falcon/Models"
)

// const SMBPath = "/samba/main/"
const SMBPath = "./Temp/"

var EmailConfig Models.EmailConfig = Models.EmailConfig{
	SMTPServer:   "smtp.gmail.com",
	SMTPPort:     465,
	Username:     "redacted@example.com",
	Password:     "REDACTED",
	FromEmail:    "redacted@example.com",
	FromName:     "Apex",
	TLSEnabled:   true,
	SkipTLSCheck: false,
}

const WhatsappGoService = "http://localhost:3000"

const WhatsAppAlertNumber = "+201061856523"
const WhatsAppGPIDFuel = "120363420442360151@g.us"
const WhatsAppGPIDEtit = "120363418908976957@g.us"
