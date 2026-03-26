package email

import (
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
		config.SESSMTPPort,
		config.SESSMTPUser,
		config.SESSMTPPassword,
	)

	return d.DialAndSend(m)
}
