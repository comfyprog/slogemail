package slogemail

import (
	"context"

	"github.com/wneessen/go-mail"
)

type Mailer struct {
	client *mail.Client
}

func NewMailer(smtpHost string, smtpPort int, username string, password string) (*Mailer, error) {
	c, err := mail.NewClient(smtpHost, mail.WithPort(smtpPort), mail.WithUsername(username), mail.WithPassword(password))
	if err != nil {
		return nil, err
	}

	return &Mailer{client: c}, nil
}

func (m *Mailer) SendPlaintextMessage(ctx context.Context, from string, to []string, subject string, body string) error {
	msg := mail.NewMsg()
	if err := msg.From(from); err != nil {
		return err
	}

	if err := msg.To(to...); err != nil {
		return err
	}

	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, body)

	return m.client.DialAndSendWithContext(ctx, msg)
}
