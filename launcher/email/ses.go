package email

import (
	"crypto/tls"
	"fmt"

	"gopkg.in/gomail.v2"
	"launcher/config"
)

func SendEmail(to, subject, body string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", config.SESFromEmail)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)

	d := gomail.NewDialer(
		config.SESSMTPHost,
		465,
		config.SESSMTPUser,
		config.SESSMTPPassword,
	)
	d.SSL = true
	d.TLSConfig = &tls.Config{
		ServerName: config.SESSMTPHost,
	}

	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}
