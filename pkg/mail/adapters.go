package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/myfoxit/goforge/pkg/awsig"
)

// ---- sendmail (local MTA) ----

func init() {
	Register("sendmail", func(cfg Config) (Mailer, error) {
		return &sendmailMailer{path: cfg.Get("sendmail.path", "/usr/sbin/sendmail")}, nil
	})
}

type sendmailMailer struct{ path string }

func (s *sendmailMailer) Name() string { return "sendmail" }

func (s *sendmailMailer) Send(ctx context.Context, m *Message) error {
	args := append([]string{"-i", "-f", m.From.Email, "--"}, m.Recipients()...)
	cmd := exec.CommandContext(ctx, s.path, args...)
	cmd.Stdin = bytes.NewReader(buildMIME(m))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mail: sendmail: %w (%s)", err, bytes.TrimSpace(out))
	}
	return nil
}

// ---- resend.com ----

func init() {
	Register("resend", func(cfg Config) (Mailer, error) {
		key := cfg.Get("resend.apiKey", "")
		if key == "" {
			return nil, fmt.Errorf("mail: resend.apiKey not configured")
		}
		return &resendMailer{apiKey: key}, nil
	})
}

type resendMailer struct{ apiKey string }

func (r *resendMailer) Name() string { return "resend" }

func (r *resendMailer) Send(ctx context.Context, m *Message) error {
	payload := map[string]any{
		"from":    m.From.String(),
		"to":      emails(m.To),
		"subject": m.Subject,
	}
	if len(m.Cc) > 0 {
		payload["cc"] = emails(m.Cc)
	}
	if len(m.Bcc) > 0 {
		payload["bcc"] = emails(m.Bcc)
	}
	if m.HTML != "" {
		payload["html"] = m.HTML
	}
	if m.Text != "" {
		payload["text"] = m.Text
	}
	if len(m.Attachments) > 0 {
		atts := make([]map[string]any, len(m.Attachments))
		for i, a := range m.Attachments {
			atts[i] = map[string]any{
				"filename": a.Filename,
				"content":  base64.StdEncoding.EncodeToString(a.Content),
			}
		}
		payload["attachments"] = atts
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return doJSON(req, "resend")
}

// ---- AWS SES (v2 API, SigV4-signed, no SDK) ----

func init() {
	Register("ses", func(cfg Config) (Mailer, error) {
		region := cfg.Get("ses.region", "")
		key := cfg.Get("ses.accessKey", "")
		secret := cfg.Get("ses.secretKey", "")
		if region == "" || key == "" || secret == "" {
			return nil, fmt.Errorf("mail: ses.region, ses.accessKey and ses.secretKey are required")
		}
		return &sesMailer{region: region, creds: awsig.Credentials{AccessKey: key, SecretKey: secret}}, nil
	})
}

type sesMailer struct {
	region string
	creds  awsig.Credentials
}

func (s *sesMailer) Name() string { return "ses" }

func (s *sesMailer) Send(ctx context.Context, m *Message) error {
	// Raw message supports attachments and full MIME control.
	payload := map[string]any{
		"FromEmailAddress": m.From.String(),
		"Destination": map[string]any{
			"ToAddresses":  emails(m.To),
			"CcAddresses":  emails(m.Cc),
			"BccAddresses": emails(m.Bcc),
		},
		"Content": map[string]any{
			"Raw": map[string]any{"Data": base64.StdEncoding.EncodeToString(buildMIME(m))},
		},
	}
	body, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("https://email.%s.amazonaws.com/v2/email/outbound-emails", s.region)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := awsig.Sign(req, body, s.creds, s.region, "ses", time.Now().UTC()); err != nil {
		return err
	}
	return doJSON(req, "ses")
}

// ---- log (dev) ----

func init() {
	Register("log", func(cfg Config) (Mailer, error) {
		return &logMailer{}, nil
	})
}

// logMailer prints emails to the log and keeps the last 50 in memory so the
// admin UI (and tests) can inspect them.
type logMailer struct{}

var (
	sentMu   sync.Mutex
	sentLog  []*Message
	sentSize = 50
)

func (l *logMailer) Name() string { return "log" }

func (l *logMailer) Send(ctx context.Context, m *Message) error {
	slog.Info("mail (log adapter)", "to", m.Recipients(), "subject", m.Subject)
	fmt.Printf("\n--- EMAIL (log adapter) ---\nTo: %s\nSubject: %s\n\n%s\n---------------------------\n",
		joinAddrs(m.To), m.Subject, m.Text)
	sentMu.Lock()
	sentLog = append(sentLog, m)
	if len(sentLog) > sentSize {
		sentLog = sentLog[len(sentLog)-sentSize:]
	}
	sentMu.Unlock()
	return nil
}

// SentMessages returns messages captured by the log adapter (newest last).
func SentMessages() []*Message {
	sentMu.Lock()
	defer sentMu.Unlock()
	return append([]*Message{}, sentLog...)
}

// ResetSentMessages clears the captured message log (tests).
func ResetSentMessages() {
	sentMu.Lock()
	sentLog = nil
	sentMu.Unlock()
}

// ---- shared http helpers ----

var httpClient = &http.Client{Timeout: 30 * time.Second}

func doJSON(req *http.Request, name string) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mail: %s request: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mail: %s returned %d: %s", name, resp.StatusCode, bytes.TrimSpace(body))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func emails(addrs []Address) []string {
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.Email
	}
	return out
}
