package mail

import (
	"encoding/base64"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/myfoxit/goforge/pkg/security"
)

// buildMIME renders a Message as an RFC 5322 message with
// multipart/alternative bodies and optional attachments.
func buildMIME(m *Message) []byte {
	var b strings.Builder
	boundaryMixed := "gf-mixed-" + security.RandomID(16)
	boundaryAlt := "gf-alt-" + security.RandomID(16)

	writeHeader := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }

	writeHeader("From", m.From.String())
	writeHeader("To", joinAddrs(m.To))
	if len(m.Cc) > 0 {
		writeHeader("Cc", joinAddrs(m.Cc))
	}
	writeHeader("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	writeHeader("Message-ID", fmt.Sprintf("<%s@goforge>", security.RandomID(20)))
	writeHeader("MIME-Version", "1.0")
	for k, v := range m.Headers {
		writeHeader(k, v)
	}

	hasAttachments := len(m.Attachments) > 0
	if hasAttachments {
		writeHeader("Content-Type", fmt.Sprintf(`multipart/mixed; boundary="%s"`, boundaryMixed))
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s\r\n", boundaryMixed)
	}

	// alternative part (text + html)
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundaryAlt)
	if m.Text != "" {
		fmt.Fprintf(&b, "--%s\r\n", boundaryAlt)
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		b.WriteString(wrapBase64(m.Text))
	}
	if m.HTML != "" {
		fmt.Fprintf(&b, "--%s\r\n", boundaryAlt)
		b.WriteString("Content-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		b.WriteString(wrapBase64(m.HTML))
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundaryAlt)

	if hasAttachments {
		for _, att := range m.Attachments {
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			fmt.Fprintf(&b, "--%s\r\n", boundaryMixed)
			fmt.Fprintf(&b, "Content-Type: %s\r\n", ct)
			fmt.Fprintf(&b, "Content-Disposition: attachment; filename=\"%s\"\r\n", strings.ReplaceAll(att.Filename, `"`, ""))
			b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
			b.WriteString(wrapBase64String(base64.StdEncoding.EncodeToString(att.Content)))
		}
		fmt.Fprintf(&b, "--%s--\r\n", boundaryMixed)
	}
	return []byte(b.String())
}

func joinAddrs(addrs []Address) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

func wrapBase64(s string) string {
	return wrapBase64String(base64.StdEncoding.EncodeToString([]byte(s)))
}

// wrapBase64String hard-wraps base64 at 76 chars per RFC 2045.
func wrapBase64String(enc string) string {
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	b.WriteString("\r\n")
	return b.String()
}
