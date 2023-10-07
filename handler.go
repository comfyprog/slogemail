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
type SendEmailFunc func(ctx context.Context, r slog.Record, logOutput string) error

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

type logEmail struct {
	ctx  context.Context
	rec  slog.Record
	text string
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
	// GetSubject is a user-defined function for making custom email subject. By default log record level name is used.
	GetSubject GetSubjectFunc
	// GetBody is a user-defined function for making custom email body. By default log record text is used.
	GetBody GetBodyFunc
	// ConnectionInfo contains information about SMTP server.
	ConnectionInfo SMTPConnectionInfo
	// QueueSize specifies how many records can be queued before logger will have to actually wait for them to be sent
	// Default: 1
	QueueSize int
}

// EmailHandler is a log/slog compatible handler that writes log records in text or json to user-provided io.Writer
// and also emails records with defined levels to specified addresses.
type EmailHandler struct {
	enabled       bool
	baseHandler   slog.Handler
	buf           *bytes.Buffer
	out           io.Writer
	mu            sync.Mutex
	emailLevel    slog.Level
	customSend    SendEmailFunc
	getSubject    GetSubjectFunc
	getBody       GetBodyFunc
	fromAddr      string
	toAddrs       []string
	json          bool
	defaultMailer *Mailer
	mailC         chan logEmail
}

// NewCustomHandler creates a new handler that prints logs to supplied io.Writer and
// also passes them to user-defined function
func NewCustomHandler(w io.Writer, opts *slog.HandlerOptions, f SendEmailFunc, json bool) *EmailHandler {
	buf := new(bytes.Buffer)
	var baseHandler slog.Handler
	if json {
		baseHandler = slog.NewJSONHandler(buf, opts)
	} else {
		baseHandler = slog.NewTextHandler(buf, opts)
	}
	handler := &EmailHandler{
		enabled:     true,
		baseHandler: baseHandler,
		buf:         buf,
		out:         w,
		customSend:  f,
	}
	return handler
}

// NewHandler creates a new handler than prints log to supplied io.Writer and
// also sends them to a simple SMTP server as an email
func NewHandler(w io.Writer, opts *slog.HandlerOptions, emailOpts EmailHandlerOpts) (*EmailHandler, func(), error) {
	buf := new(bytes.Buffer)
	var baseHandler slog.Handler
	if emailOpts.JSON {
		baseHandler = slog.NewJSONHandler(buf, opts)
	} else {
		baseHandler = slog.NewTextHandler(buf, opts)
	}

	if emailOpts.QueueSize == 0 {
		emailOpts.QueueSize = 1
	}

	handler := &EmailHandler{
		enabled:     true,
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

	mailer, err := NewMailer(emailOpts.ConnectionInfo.Host, emailOpts.ConnectionInfo.Port,
		emailOpts.ConnectionInfo.Username, emailOpts.ConnectionInfo.Password)
	if err != nil {
		return handler, nil, err
	}
	handler.defaultMailer = mailer

	handler.mailC = make(chan logEmail, emailOpts.QueueSize)

	go func() {
		for e := range handler.mailC {
			handler.send(e.ctx, e.rec, e.text)
		}
	}()

	closeFunc := func() {
		handler.mu.Lock()
		defer handler.mu.Unlock()
		handler.enabled = false
		close(handler.mailC)
	}

	return handler, closeFunc, nil
}

func (h *EmailHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.enabled && h.baseHandler.Enabled(ctx, level)
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
	_, err := fmt.Fprintf(h.out, "%s", text)
	if err != nil {
		return err
	}

	if r.Level >= h.emailLevel {
		if h.customSend != nil {
			return h.customSend(ctx, r, text)
		}
		h.mailC <- logEmail{
			ctx:  ctx,
			rec:  r,
			text: text,
		}
	}

	return nil
}

func (h *EmailHandler) send(ctx context.Context, r slog.Record, text string) error {

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
			var err error
			body, err = prettifyJSON(text)
			if err != nil {
				return err
			}
		} else {
			body = text
		}
	}

	return h.defaultMailer.SendPlaintextMessage(ctx, h.fromAddr, h.toAddrs, subject, body)
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
