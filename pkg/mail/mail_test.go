package mail

import (
	"context"
	"strings"
	"testing"
)

func TestRenderTemplate(t *testing.T) {
	subject, html, err := RenderTemplate("verification", TemplateData{
		AppName:   "TestApp",
		AppURL:    "https://test.app",
		ActionURL: "https://test.app/verify?token=abc",
		Name:      "Ada",
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(subject, "TestApp") {
		t.Fatalf("subject = %s", subject)
	}
	for _, want := range []string{"Ada", "https://test.app/verify?token=abc", "<html>"} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q", want)
		}
	}

	// overrides
	subject, html, err = RenderTemplate("verification", TemplateData{AppName: "X"}, "Custom {{.AppName}}", "<p>Body</p>")
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Custom X" || !strings.Contains(html, "<p>Body</p>") {
		t.Fatalf("override failed: %s / %s", subject, html)
	}

	if _, _, err := RenderTemplate("nope", TemplateData{}, "", ""); err == nil {
		t.Fatal("unknown template accepted")
	}
}

func TestLogAdapterAndValidation(t *testing.T) {
	ResetSentMessages()
	m, err := New("log", Config{})
	if err != nil {
		t.Fatal(err)
	}
	msg := &Message{
		From:    Address{Email: "from@x.dev"},
		To:      []Address{{Email: "to@x.dev", Name: "To"}},
		Subject: "Hi",
		HTML:    "<p>Hello <b>you</b></p>",
	}
	if err := Send(context.Background(), m, msg); err != nil {
		t.Fatal(err)
	}
	sent := SentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent = %d", len(sent))
	}
	if sent[0].Text == "" || strings.Contains(sent[0].Text, "<p>") {
		t.Fatalf("text fallback = %q", sent[0].Text)
	}

	// validation
	if err := Send(context.Background(), m, &Message{From: Address{Email: "a@b.c"}}); err == nil {
		t.Fatal("invalid message accepted")
	}
}

func TestBuildMIME(t *testing.T) {
	raw := string(buildMIME(&Message{
		From:    Address{Name: "Sender", Email: "s@x.dev"},
		To:      []Address{{Email: "r@x.dev"}},
		Subject: "Sübject with ümlauts",
		HTML:    "<p>html</p>",
		Text:    "text",
		Attachments: []Attachment{
			{Filename: "a.txt", ContentType: "text/plain", Content: []byte("attached")},
		},
	}))
	for _, want := range []string{
		"From: \"Sender\" <s@x.dev>",
		"multipart/mixed",
		"multipart/alternative",
		"text/html; charset=utf-8",
		"Content-Disposition: attachment; filename=\"a.txt\"",
		"MIME-Version: 1.0",
		"=?utf-8?q?", // encoded subject
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("mime missing %q in:\n%s", want, raw)
		}
	}
}

func TestAdapterRegistry(t *testing.T) {
	names := Adapters()
	for _, want := range []string{"smtp", "sendmail", "resend", "ses", "log"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("adapter %q not registered (have %v)", want, names)
		}
	}
	if _, err := New("resend", Config{}); err == nil {
		t.Fatal("resend without key accepted")
	}
	if _, err := New("ghost", Config{}); err == nil {
		t.Fatal("unknown adapter accepted")
	}
}
