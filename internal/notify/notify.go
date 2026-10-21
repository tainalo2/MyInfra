package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Alerts are optional: MyInfra starts with no e-mail nor ntfy configured.
func Enabled() bool {
	return strings.TrimSpace(os.Getenv("MYINFRA_ALERT_NTFY_URL")) != "" ||
		(strings.TrimSpace(os.Getenv("MYINFRA_ALERT_SMTP_HOST")) != "" && strings.TrimSpace(os.Getenv("MYINFRA_ALERT_EMAIL_TO")) != "")
}

// Send delivers a short message when a channel is configured; otherwise it
// does nothing.
func Send(ctx context.Context, title, body string) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" && body == "" {
		return
	}
	if url := strings.TrimSpace(os.Getenv("MYINFRA_ALERT_NTFY_URL")); url != "" {
		_ = sendNtfy(ctx, url, title, body)
	}
	if host := strings.TrimSpace(os.Getenv("MYINFRA_ALERT_SMTP_HOST")); host != "" {
		to := strings.TrimSpace(os.Getenv("MYINFRA_ALERT_EMAIL_TO"))
		if to != "" {
			_ = sendSMTP(host, to, title, body)
		}
	}
}

func sendNtfy(ctx context.Context, url, title, body string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return err
	}
	if title != "" {
		req.Header.Set("Title", title)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("ntfy: %s", res.Status)
	}
	return nil
}

func sendSMTP(host, to, title, body string) error {
	from := strings.TrimSpace(os.Getenv("MYINFRA_ALERT_EMAIL_FROM"))
	if from == "" {
		from = "myinfra@localhost"
	}
	port := strings.TrimSpace(os.Getenv("MYINFRA_ALERT_SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	user := os.Getenv("MYINFRA_ALERT_SMTP_USER")
	pass := os.Getenv("MYINFRA_ALERT_SMTP_PASSWORD")
	addr := host + ":" + port
	msg := "From: " + from + "\r\nTo: " + to + "\r\nSubject: " + title + "\r\n\r\n" + body + "\r\n"
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// ValidateEmail is a loose check for alert recipients in forms later.
func ValidateEmail(raw string) bool {
	_, err := mail.ParseAddress(strings.TrimSpace(raw))
	return err == nil
}
