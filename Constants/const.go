package Constants

import (
	"Falcon/Models"
)

// const SMBPath = "/samba/main/"
const SMBPath = "./Temp/"

var EmailConfig Models.EmailConfig = Models.EmailConfig{
	SMTPServer:   "smtp.gmail.com",
	SMTPPort:     465,
	Username:     "shawketibrahim7@gmail.com",
	Password:     "mtok pjnf stai hbuy",
	FromEmail:    "shawketibrahim7@gmail.com",
	FromName:     "Apex",
	TLSEnabled:   true,
	SkipTLSCheck: false,
}
