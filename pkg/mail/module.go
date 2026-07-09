package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/myfoxit/goforge/pkg/core"
)

// Module wires mail into an app: a settings section for adapter selection
// and credentials, and helpers to send templated messages.
type Module struct{}

func (Module) ID() string { return "mail" }

func (Module) Register(app *core.App) error {
	app.Settings().RegisterSection(core.SettingsSection{
		ID: "mail", Title: "Email", Order: 20,
		Fields: []core.SettingsField{
			{Key: "mail.adapter", Label: "Adapter", Type: "select", Options: Adapters(), Default: "log",
				Help: "log prints to the app log (development)."},
			{Key: "mail.fromName", Label: "Sender name", Type: "text", Placeholder: "My App"},
			{Key: "mail.fromAddress", Label: "Sender address", Type: "text", Placeholder: "noreply@example.com"},

			{Key: "smtp.host", Label: "SMTP host", Type: "text", Placeholder: "smtp.example.com"},
			{Key: "smtp.port", Label: "SMTP port", Type: "text", Default: "587"},
			{Key: "smtp.username", Label: "SMTP username", Type: "text"},
			{Key: "smtp.password", Label: "SMTP password", Type: "secret"},
			{Key: "smtp.security", Label: "SMTP security", Type: "select", Options: []string{"starttls", "tls", "none"}, Default: "starttls"},

			{Key: "sendmail.path", Label: "sendmail path", Type: "text", Default: "/usr/sbin/sendmail"},

			{Key: "resend.apiKey", Label: "Resend API key", Type: "secret"},

			{Key: "ses.region", Label: "SES region", Type: "text", Placeholder: "eu-central-1"},
			{Key: "ses.accessKey", Label: "SES access key", Type: "text"},
			{Key: "ses.secretKey", Label: "SES secret key", Type: "secret"},
		},
	})
	return nil
}

// FromApp builds the configured Mailer from the app settings.
func FromApp(app *core.App) (Mailer, error) {
	s := app.Settings()
	adapter := s.String("mail.adapter")
	if adapter == "" {
		adapter = "log"
	}
	cfg := Config{}
	for _, key := range []string{
		"smtp.host", "smtp.port", "smtp.username", "smtp.password", "smtp.security",
		"sendmail.path", "resend.apiKey", "ses.region", "ses.accessKey", "ses.secretKey",
	} {
		cfg[key] = s.String(key)
	}
	return New(adapter, cfg)
}

// DefaultFrom returns the configured sender, falling back to a noreply
// address derived from the app URL.
func DefaultFrom(app *core.App) Address {
	s := app.Settings()
	from := Address{Name: s.String("mail.fromName"), Email: s.String("mail.fromAddress")}
	if from.Name == "" {
		from.Name = app.AppName()
	}
	if from.Email == "" {
		host := app.BaseURL()
		host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
		if i := strings.IndexAny(host, ":/"); i > 0 {
			host = host[:i]
		}
		if host == "" || host == "localhost" {
			host = "localhost.local"
		}
		from.Email = "noreply@" + host
	}
	return from
}

// SendMessage delivers msg via the configured adapter, applying the default
// sender when unset.
func SendMessage(ctx context.Context, app *core.App, msg *Message) error {
	m, err := FromApp(app)
	if err != nil {
		return err
	}
	if msg.From.Email == "" {
		msg.From = DefaultFrom(app)
	}
	if err := Send(ctx, m, msg); err != nil {
		app.Log().Error("mail send failed", "adapter", m.Name(), "to", msg.Recipients(), "err", err)
		return err
	}
	app.Log().Info("mail sent", "adapter", m.Name(), "to", msg.Recipients(), "subject", msg.Subject)
	return nil
}

// SendTemplate renders a named template and delivers it to one recipient.
// Settings keys mail.template.<name>.subject / .body override the defaults.
func SendTemplate(ctx context.Context, app *core.App, to Address, templateName string, data TemplateData) error {
	if data.AppName == "" {
		data.AppName = app.AppName()
	}
	if data.AppURL == "" {
		data.AppURL = app.BaseURL()
	}
	s := app.Settings()
	subject, html, err := RenderTemplate(templateName, data,
		s.String(fmt.Sprintf("mail.template.%s.subject", templateName)),
		s.String(fmt.Sprintf("mail.template.%s.body", templateName)),
	)
	if err != nil {
		return err
	}
	return SendMessage(ctx, app, &Message{
		To:      []Address{to},
		Subject: subject,
		HTML:    html,
	})
}
