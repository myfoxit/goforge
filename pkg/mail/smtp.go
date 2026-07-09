package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

func init() {
	Register("smtp", func(cfg Config) (Mailer, error) {
		host := cfg.Get("smtp.host", "")
		if host == "" {
			return nil, fmt.Errorf("mail: smtp.host not configured")
		}
		return &smtpMailer{
			host:     host,
			port:     cfg.Get("smtp.port", "587"),
			username: cfg.Get("smtp.username", ""),
			password: cfg.Get("smtp.password", ""),
			// security: starttls (default) | tls | none
			security: cfg.Get("smtp.security", "starttls"),
		}, nil
	})
}

type smtpMailer struct {
	host, port         string
	username, password string
	security           string
}

func (s *smtpMailer) Name() string { return "smtp" }

func (s *smtpMailer) Send(ctx context.Context, m *Message) error {
	addr := net.JoinHostPort(s.host, s.port)
	dialer := &net.Dialer{Timeout: 15 * time.Second}

	var conn net.Conn
	var err error
	if s.security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: smtp dial: %w", err)
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: smtp handshake: %w", err)
	}
	defer client.Close()

	if s.security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("mail: starttls: %w", err)
			}
		} else {
			return errors.New("mail: server does not support STARTTLS (set smtp.security=none to allow plaintext)")
		}
	}

	if s.username != "" {
		auth := s.pickAuth(client)
		if auth == nil {
			return errors.New("mail: no supported SMTP AUTH mechanism")
		}
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mail: smtp auth: %w", err)
		}
	}

	if err := client.Mail(m.From.Email); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	for _, rcpt := range m.Recipients() {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("mail: RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildMIME(m)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (s *smtpMailer) pickAuth(client *smtp.Client) smtp.Auth {
	ok, mechs := client.Extension("AUTH")
	if !ok {
		return nil
	}
	switch {
	case contains(mechs, "PLAIN"):
		return smtp.PlainAuth("", s.username, s.password, s.host)
	case contains(mechs, "LOGIN"):
		return &loginAuth{username: s.username, password: s.password}
	case contains(mechs, "CRAM-MD5"):
		return smtp.CRAMMD5Auth(s.username, s.password)
	}
	return nil
}

func contains(haystack, needle string) bool {
	for _, part := range splitFields(haystack) {
		if part == needle {
			return true
		}
	}
	return false
}

func splitFields(s string) []string {
	var out []string
	field := ""
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if field != "" {
				out = append(out, field)
				field = ""
			}
			continue
		}
		field += string(r)
	}
	if field != "" {
		out = append(out, field)
	}
	return out
}

// loginAuth implements the legacy AUTH LOGIN mechanism (Office365 et al).
type loginAuth struct{ username, password string }

func (a *loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte{}, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch string(fromServer) {
	case "Username:":
		return []byte(a.username), nil
	case "Password:":
		return []byte(a.password), nil
	}
	return []byte(a.username), nil
}
