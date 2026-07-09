package mail

import (
	"bytes"
	"fmt"
	"html/template"
)

// TemplateData is passed to email templates.
type TemplateData struct {
	AppName   string
	AppURL    string
	ActionURL string
	Token     string
	Code      string // OTP / MFA codes
	Name      string
	Email     string
	Extra     map[string]any
}

const layoutHTML = `<!doctype html>
<html>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="padding:32px 16px">
<tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:460px;background:#ffffff;border-radius:12px;border:1px solid #e4e4e7">
<tr><td style="padding:32px">
<h1 style="margin:0 0 16px;font-size:18px;color:#18181b">{{.AppName}}</h1>
<div style="font-size:14px;line-height:1.6;color:#3f3f46">{{.Body}}</div>
</td></tr>
</table>
<p style="font-size:12px;color:#a1a1aa;margin-top:16px">Sent by {{.AppName}} · <a href="{{.AppURL}}" style="color:#a1a1aa">{{.AppURL}}</a></p>
</td></tr>
</table>
</body>
</html>`

var defaultTemplates = map[string]struct {
	Subject string
	Body    string
}{
	"verification": {
		Subject: "Verify your {{.AppName}} email",
		Body: `<p>Hello{{if .Name}} {{.Name}}{{end}},</p>
<p>Thanks for joining {{.AppName}}! Click the button below to verify your email address.</p>
<p style="margin:24px 0"><a href="{{.ActionURL}}" style="background:#18181b;color:#fafafa;padding:10px 18px;border-radius:8px;text-decoration:none;display:inline-block">Verify email</a></p>
<p>If the button doesn't work, copy this link into your browser:<br><a href="{{.ActionURL}}">{{.ActionURL}}</a></p>
<p>If you didn't create this account, you can safely ignore this email.</p>`,
	},
	"password-reset": {
		Subject: "Reset your {{.AppName}} password",
		Body: `<p>Hello{{if .Name}} {{.Name}}{{end}},</p>
<p>Someone requested a password reset for your account. Click below to choose a new password. The link expires in 30 minutes.</p>
<p style="margin:24px 0"><a href="{{.ActionURL}}" style="background:#18181b;color:#fafafa;padding:10px 18px;border-radius:8px;text-decoration:none;display:inline-block">Reset password</a></p>
<p>If the button doesn't work, copy this link into your browser:<br><a href="{{.ActionURL}}">{{.ActionURL}}</a></p>
<p>If you didn't request this, you can safely ignore this email — your password stays unchanged.</p>`,
	},
	"email-change": {
		Subject: "Confirm your new {{.AppName}} email address",
		Body: `<p>Hello{{if .Name}} {{.Name}}{{end}},</p>
<p>Confirm your new email address for {{.AppName}}:</p>
<p style="margin:24px 0"><a href="{{.ActionURL}}" style="background:#18181b;color:#fafafa;padding:10px 18px;border-radius:8px;text-decoration:none;display:inline-block">Confirm new email</a></p>
<p>If you didn't request this change, please contact support immediately.</p>`,
	},
	"otp": {
		Subject: "Your {{.AppName}} login code",
		Body: `<p>Hello{{if .Name}} {{.Name}}{{end}},</p>
<p>Your one-time login code is:</p>
<p style="font-size:28px;letter-spacing:6px;font-weight:700;color:#18181b;margin:24px 0">{{.Code}}</p>
<p>The code expires in 5 minutes. If you didn't try to log in, change your password.</p>`,
	},
	"invite": {
		Subject: "You have been invited to {{.AppName}}",
		Body: `<p>Hello,</p>
<p>You have been invited to join <strong>{{.AppName}}</strong>{{if .Extra.org}} ({{.Extra.org}}){{end}}.</p>
<p style="margin:24px 0"><a href="{{.ActionURL}}" style="background:#18181b;color:#fafafa;padding:10px 18px;border-radius:8px;text-decoration:none;display:inline-block">Accept invitation</a></p>
<p>If the button doesn't work, copy this link into your browser:<br><a href="{{.ActionURL}}">{{.ActionURL}}</a></p>`,
	},
	"test": {
		Subject: "{{.AppName}} test email",
		Body:    `<p>This is a test email from <strong>{{.AppName}}</strong>. Your mail configuration works. 🎉</p>`,
	},
}

// TemplateNames lists available email templates.
func TemplateNames() []string {
	return []string{"verification", "password-reset", "email-change", "otp", "invite", "test"}
}

// RenderTemplate renders a named template (with optional subject/body
// overrides, e.g. from settings) into a subject + html body.
func RenderTemplate(name string, data TemplateData, subjectOverride, bodyOverride string) (subject, html string, err error) {
	tpl, ok := defaultTemplates[name]
	if !ok {
		return "", "", fmt.Errorf("mail: unknown template %q", name)
	}
	subjectSrc := tpl.Subject
	if subjectOverride != "" {
		subjectSrc = subjectOverride
	}
	bodySrc := tpl.Body
	if bodyOverride != "" {
		bodySrc = bodyOverride
	}
	if data.Extra == nil {
		data.Extra = map[string]any{}
	}

	subject, err = renderString("subject", subjectSrc, data)
	if err != nil {
		return "", "", err
	}
	body, err := renderString("body", bodySrc, data)
	if err != nil {
		return "", "", err
	}
	layout, err := template.New("layout").Parse(layoutHTML)
	if err != nil {
		return "", "", err
	}
	var out bytes.Buffer
	err = layout.Execute(&out, map[string]any{
		"AppName": data.AppName,
		"AppURL":  data.AppURL,
		"Body":    template.HTML(body),
	})
	return subject, out.String(), err
}

func renderString(name, src string, data TemplateData) (string, error) {
	t, err := template.New(name).Parse(src)
	if err != nil {
		return "", fmt.Errorf("mail: parse %s template: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("mail: render %s template: %w", name, err)
	}
	return buf.String(), nil
}
