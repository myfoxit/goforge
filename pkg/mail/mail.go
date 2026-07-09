// Package mail sends transactional email through pluggable adapters:
// smtp, sendmail, resend, ses and log (dev). The active adapter and its
// credentials are runtime settings, so switching providers is a config
// change — no rebuild.
package mail

import (
	"context"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"sync"
)

// Address is a display name + email pair.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (a Address) String() string {
	if a.Name == "" {
		return a.Email
	}
	return (&mail.Address{Name: a.Name, Address: a.Email}).String()
}

// Attachment is an inline file.
type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// Message is one outgoing email.
type Message struct {
	From        Address
	To          []Address
	Cc          []Address
	Bcc         []Address
	Subject     string
	HTML        string
	Text        string
	Headers     map[string]string
	Attachments []Attachment
}

// Recipients returns all destination addresses (to+cc+bcc).
func (m *Message) Recipients() []string {
	out := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	for _, list := range [][]Address{m.To, m.Cc, m.Bcc} {
		for _, a := range list {
			out = append(out, a.Email)
		}
	}
	return out
}

func (m *Message) validate() error {
	if m.From.Email == "" {
		return fmt.Errorf("mail: missing From address")
	}
	if len(m.To) == 0 {
		return fmt.Errorf("mail: missing recipients")
	}
	if m.Subject == "" {
		return fmt.Errorf("mail: missing subject")
	}
	if m.HTML == "" && m.Text == "" {
		return fmt.Errorf("mail: empty body")
	}
	return nil
}

// Mailer delivers messages.
type Mailer interface {
	Name() string
	Send(ctx context.Context, m *Message) error
}

// Config carries adapter settings (flat string map, from the settings store).
type Config map[string]string

func (c Config) Get(key, def string) string {
	if v, ok := c[key]; ok && v != "" {
		return v
	}
	return def
}

// Factory builds a Mailer from config.
type Factory func(cfg Config) (Mailer, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register adds an adapter factory (called from adapter init()).
func Register(name string, f Factory) {
	mu.Lock()
	factories[name] = f
	mu.Unlock()
}

// Adapters lists registered adapter names.
func Adapters() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for name := range factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// New builds a named adapter.
func New(name string, cfg Config) (Mailer, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mail: unknown adapter %q (available: %s)", name, strings.Join(Adapters(), ", "))
	}
	return f(cfg)
}

// Send validates and delivers a message through the given adapter.
func Send(ctx context.Context, m Mailer, msg *Message) error {
	if err := msg.validate(); err != nil {
		return err
	}
	if msg.Text == "" {
		msg.Text = htmlToText(msg.HTML)
	}
	return m.Send(ctx, msg)
}

// htmlToText produces a crude plaintext fallback.
func htmlToText(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	lines := strings.Split(b.String(), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
