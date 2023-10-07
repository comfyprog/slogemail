package slogemail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// SendEmailFunc describes function that user has to implement to fully control mailing process
type SendEmailFunc func(ctx context.Context, from string, to []string, subject string, body string) error

// GetSubjectFunc is a function that accepts an slog record and rendered output
// from slog's standard text or json handler and returns a subject for a letter
type GetSubjectFunc func(ctx context.Context, r slog.Record, logOutput string) string

// GetBodyFunc is a function that accepts an slog record and rendered output
// from slog's standard text or json handler and returns a body for a letter
type GetBodyFunc func(ctx context.Context, r slog.Record, logOutput string) string

// SMTPConnectionInfo cotains information sufficient to connect to a generic SMTP server
type SMTPConnectionInfo struct {
	// Host
	Host string
	// Port
	Port int
	// Username
	Username string
	// Password
	Password string
}

// EmailHandlerOpts contains options specific to EmailHandler
// If SendEmail is not provided, ConnectionInfo will be used
type EmailHandlerOpts struct {
	// FromAddr is a from email address
	FromAddr string
	// ToAddrs is a slice of emails that log records will be sent to
	ToAddrs []string
	// JSON sets log record format (true = pretty JSON, false = text)
	JSON bool
	// Level determines the minimum log level at which emails will be sent. Can be different from level in `opts` parameter in NewHandler
	Level slog.Level
	// SendEmail is a user-defined function that processes log data meant to be emailed.
	// Useful for cases where simple SMTP connection is not sufficient or extra control is needed.
	//
	SendEmail SendEmailFunc
	// GetSubject is a user-defined function for making custom email subject. By default log record level name is used.
	GetSubject GetSubjectFunc
	// GetBody is a user-defined function for making custom email body. By default log record text is used.
	GetBody GetBodyFunc
	// ConnectionInfo contains information about SMTP server.
	// Required if SendEmail is nil.
	ConnectionInfo SMTPConnectionInfo
}

// EmailHandler is a log/slog compatible handler that writes log records in text or json to user-provided io.Writer
// and also emails records with defined levels to specified addresses.
type EmailHandler struct {
	baseHandler   slog.Handler
	buf           *bytes.Buffer
	out           io.Writer
	mu            sync.Mutex
	emailLevel    slog.Level
	sendEmail     SendEmailFunc
	getSubject    GetSubjectFunc
	getBody       GetBodyFunc
	fromAddr      string
	toAddrs       []string
	json          bool
	defaultMailer *Mailer
}

func NewHandler(w io.Writer, opts *slog.HandlerOptions, emailOpts EmailHandlerOpts) (*EmailHandler, error) {
	buf := new(bytes.Buffer)
	var baseHandler slog.Handler
	if emailOpts.JSON {
		baseHandler = slog.NewJSONHandler(buf, opts)
	} else {
		baseHandler = slog.NewTextHandler(buf, opts)
	}
	handler := &EmailHandler{
		baseHandler: baseHandler,
		buf:         buf,
		out:         w,
		emailLevel:  emailOpts.Level,
		fromAddr:    emailOpts.FromAddr,
		toAddrs:     emailOpts.ToAddrs,
		getSubject:  emailOpts.GetSubject,
		getBody:     emailOpts.GetBody,
		json:        emailOpts.JSON,
	}

	if emailOpts.SendEmail != nil {
		handler.sendEmail = emailOpts.SendEmail
	} else {
		mailer, err := NewMailer(emailOpts.ConnectionInfo.Host, emailOpts.ConnectionInfo.Port,
			emailOpts.ConnectionInfo.Username, emailOpts.ConnectionInfo.Password)
		if err != nil {
			return handler, err
		}
		handler.defaultMailer = mailer
		handler.sendEmail = handler.sendEmailDefault
	}

	return handler, nil
}

func (h *EmailHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.baseHandler.Enabled(ctx, level)
}

func prettifyJSON(str string) (string, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(str), "", "    "); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func (h *EmailHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.baseHandler.Handle(ctx, r); err != nil {
		return err
	}
	text := h.buf.String()
	h.buf.Reset()
	_, err := fmt.Fprintf(h.out, "MY: %s", text)
	if err != nil {
		return err
	}

	if r.Level >= h.emailLevel {
		var subject, body string
		if h.getSubject != nil {
			subject = h.getSubject(ctx, r, text)
		} else {
			subject = r.Level.String()
		}

		if h.getBody != nil {
			body = h.getBody(ctx, r, text)
		} else {
			if h.json {
				body, err = prettifyJSON(text)
				if err != nil {
					return err
				}
			} else {
				body = text
			}
		}

		if err := h.sendEmail(ctx, h.fromAddr, h.toAddrs, subject, body); err != nil {
			return err
		}
	}

	return nil
}

func (h *EmailHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.baseHandler = h.baseHandler.WithAttrs(attrs)
	return h
}

func (h *EmailHandler) WithGroup(name string) slog.Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.baseHandler = h.baseHandler.WithGroup(name)
	return h
}

func (h *EmailHandler) sendEmailDefault(ctx context.Context, from string, to []string, subject string, body string) error {
	return h.defaultMailer.SendPlaintextMessage(ctx, from, to, subject, body)
}
