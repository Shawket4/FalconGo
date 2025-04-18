package email

import (
	"Falcon/Models"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// SendEmail sends an email using the provided configuration and message details
func SendEmail(config Models.EmailConfig, message Models.EmailMessage) error {
	// Build email headers
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("%s <%s>", config.FromName, config.FromEmail)
	headers["To"] = strings.Join(message.To, ", ")
	headers["Subject"] = message.Subject

	if len(message.CC) > 0 {
		headers["Cc"] = strings.Join(message.CC, ", ")
	}

	if message.IsHTML {
		headers["MIME-Version"] = "1.0"
		headers["Content-Type"] = "text/html; charset=UTF-8"
	} else {
		headers["Content-Type"] = "text/plain; charset=UTF-8"
	}

	// Build the message
	var messageBody strings.Builder
	for key, value := range headers {
		messageBody.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}

	messageBody.WriteString("\r\n")
	messageBody.WriteString(message.Body)

	// Set up authentication
	auth := smtp.PlainAuth("", config.Username, config.Password, config.SMTPServer)

	// Create recipient list (to, cc, bcc)
	var recipients []string
	recipients = append(recipients, message.To...)
	recipients = append(recipients, message.CC...)
	recipients = append(recipients, message.BCC...)

	// Set up connection
	serverAddr := fmt.Sprintf("%s:%d", config.SMTPServer, config.SMTPPort)

	// Send the email
	if config.TLSEnabled {
		// Create TLS config
		tlsConfig := &tls.Config{
			ServerName:         config.SMTPServer,
			InsecureSkipVerify: config.SkipTLSCheck,
		}

		// Connect to the SMTP server with TLS
		conn, err := tls.Dial("tcp", serverAddr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTP server: %v", err)
		}
		defer conn.Close()

		// Create SMTP client
		client, err := smtp.NewClient(conn, config.SMTPServer)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %v", err)
		}
		defer client.Close()

		// Authenticate
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %v", err)
		}

		// Set the sender and recipients
		if err = client.Mail(config.FromEmail); err != nil {
			return fmt.Errorf("failed to set sender: %v", err)
		}

		for _, recipient := range recipients {
			if err = client.Rcpt(recipient); err != nil {
				return fmt.Errorf("failed to add recipient %s: %v", recipient, err)
			}
		}

		// Send the email body
		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("failed to open data connection: %v", err)
		}

		_, err = w.Write([]byte(messageBody.String()))
		if err != nil {
			return fmt.Errorf("failed to write email body: %v", err)
		}

		err = w.Close()
		if err != nil {
			return fmt.Errorf("failed to close data connection: %v", err)
		}

		return client.Quit()
	} else {
		// Standard SMTP (non-TLS)
		err := smtp.SendMail(
			serverAddr,
			auth,
			config.FromEmail,
			recipients,
			[]byte(messageBody.String()),
		)
		return err
	}
}
