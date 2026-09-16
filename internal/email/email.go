// Package email provides a transport abstraction for outbound ProductCrew
// notification email, so callers depend on a small ProductCrew interface
// instead of binding directly to the low-level net/smtp package.
package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Message is a single outbound notification email.
type Message struct {
	To          string
	FromAddress string
	FromName    string
	Subject     string
	Body        string
}

// Security identifies how the SMTP connection is secured.
type Security string

const (
	SecurityNone     Security = "none"
	SecurityStartTLS Security = "starttls"
	SecurityTLS      Security = "tls"
)

// SMTPConfig configures the v1 SMTP-backed Transport implementation.
type SMTPConfig struct {
	Host     string
	Port     int
	Security Security
	Username string
	Password string
}

// Transport sends a single Message through a configured provider. SMTP is the
// v1 implementation; future provider adapters can implement the same
// interface without touching callers.
type Transport interface {
	Send(ctx context.Context, msg Message) error
}

type smtpTransport struct {
	config      SMTPConfig
	dialTimeout time.Duration
}

// NewSMTPTransport builds the v1 SMTP Transport.
func NewSMTPTransport(config SMTPConfig) Transport {
	return &smtpTransport{config: config, dialTimeout: 15 * time.Second}
}

func (t *smtpTransport) Send(ctx context.Context, msg Message) error {
	addr := fmt.Sprintf("%s:%d", t.config.Host, t.config.Port)
	dialer := &net.Dialer{Timeout: t.dialTimeout}

	var conn net.Conn
	var err error
	if t.config.Security == SecurityTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: t.config.Host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, t.config.Host)
	if err != nil {
		return fmt.Errorf("initialize SMTP session: %w", err)
	}
	defer client.Close()

	if t.config.Security == SecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: t.config.Host}); err != nil {
			return fmt.Errorf("TLS handshake failed: %w", err)
		}
	}

	if strings.TrimSpace(t.config.Username) != "" {
		auth := smtp.PlainAuth("", t.config.Username, t.config.Password, t.config.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication rejected: %w", err)
		}
	}

	if err := client.Mail(msg.FromAddress); err != nil {
		return fmt.Errorf("SMTP sender rejected: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("SMTP recipient rejected: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP data stream: %w", err)
	}
	fromHeader := msg.FromAddress
	if strings.TrimSpace(msg.FromName) != "" {
		fromHeader = fmt.Sprintf("%s <%s>", msg.FromName, msg.FromAddress)
	}
	body := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n%s\r\n",
		fromHeader, msg.To, msg.Subject, msg.Body,
	)
	if _, err := writer.Write([]byte(body)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize SMTP message: %w", err)
	}
	return client.Quit()
}
