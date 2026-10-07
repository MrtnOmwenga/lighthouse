// Package alert emails the owner when one of their monitors opens or resolves an incident.
// Sandboxes never send email: only the owner's tenant is notified.
package alert

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
)

// Mailer sends plain-text email over SMTP. It insists on TLS: implicit TLS on port 465, otherwise
// STARTTLS, which must be offered. (Tests may allow a plain connection to a local fake server.)
type Mailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
	Timeout  time.Duration

	allowPlain bool // tests only
	now        func() time.Time
}

var ErrNoTLS = errors.New("the SMTP server doesn't offer STARTTLS; refusing to send credentials in the clear")

// Send delivers one message to every recipient.
func (m *Mailer) Send(ctx context.Context, subject, body string) error {
	timeout := m.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	tlsConfig := &tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}
	if m.Port == 465 {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()
	if m.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		} else if !m.allowPlain {
			return ErrNoTLS
		}
	}
	if m.Username != "" {
		if err := c.Auth(auth{m.Username, m.Password}); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(m.From); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	for _, to := range m.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("smtp to %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(m.message(subject, body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	return c.Quit()
}

// auth is SMTP PLAIN over a connection that is already encrypted (Send has made sure of that).
// net/smtp's PlainAuth refuses non-TLS connections except to localhost, which is what the test
// server is; this lets Send's own TLS check be the one rule.
type auth struct{ username, password string }

func (a auth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}
func (a auth) Next([]byte, bool) ([]byte, error) { return nil, nil }

func (m *Mailer) message(subject, body string) []byte {
	now := time.Now
	if m.now != nil {
		now = m.now
	}
	var b strings.Builder
	header := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	header("From", m.From)
	header("To", strings.Join(m.To, ", "))
	header("Subject", mimeHeader(subject))
	header("Date", now().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	// net/smtp's data writer handles dot-stuffing; line endings are made CRLF here.
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}

// mimeHeader encodes a header value that isn't plain ASCII, and strips line breaks from it, so a
// monitor name can never inject extra headers.
func mimeHeader(v string) string {
	v = strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
	for _, r := range v {
		if r > 126 {
			return mime.QEncoding.Encode("utf-8", v)
		}
	}
	return v
}

// Notifier turns incident changes into email, for one tenant only.
type Notifier struct {
	Mailer    *Mailer
	Tenant    string // the owner's tenant; every other tenant is ignored
	PublicURL string
	Log       *slog.Logger
}

// Notify implements monitor.Scheduler.Notify.
func (n *Notifier) Notify(ctx context.Context, c monitor.Change) {
	if c.TenantID != n.Tenant {
		return
	}
	subject, body := Compose(c, n.PublicURL)
	if err := n.Mailer.Send(ctx, subject, body); err != nil {
		n.Log.Error("alert email failed", "incident", c.IncidentID, "err", err)
		return
	}
	n.Log.Info("alert emailed", "incident", c.IncidentID, "opened", c.Opened)
}

// Compose writes the email for an incident change.
func Compose(c monitor.Change, publicURL string) (subject, body string) {
	link := publicURL + "/console/incidents/" + c.IncidentID
	var b strings.Builder
	if c.Opened {
		subject = "[Lighthouse] " + c.Title
		fmt.Fprintf(&b, "%s failed several checks in a row", c.Monitor)
		if c.Failure != "" {
			fmt.Fprintf(&b, " (%s)", c.Failure)
		}
		fmt.Fprintf(&b, ", so Lighthouse opened an incident at %s.\n\n", c.At.UTC().Format("15:04 MST, 2 Jan"))
	} else {
		subject = "[Lighthouse] Resolved: " + c.Title
		fmt.Fprintf(&b, "%s recovered at %s. The incident lasted %s and is now resolved.\n\n",
			c.Monitor, c.At.UTC().Format("15:04 MST, 2 Jan"), c.At.Sub(c.StartedAt).Round(time.Second))
	}
	fmt.Fprintf(&b, "Incident: %s\n", link)
	if c.Public {
		fmt.Fprintf(&b, "Public status page: %s/status/incidents/%s\n", publicURL, c.IncidentID)
	}
	b.WriteString("\n— Lighthouse\n")
	return subject, b.String()
}

// TagOpened emails the owner that a link carrying a ?ref= tag was opened (the first time that
// day): which tag, where it landed, on what kind of device. Nothing identifies the visitor.
func TagOpened(m *Mailer, publicURL string, log *slog.Logger) func(ctx context.Context, tag, page, device string) {
	return func(ctx context.Context, tag, page, device string) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		subject := fmt.Sprintf("Lighthouse: the link tagged %q was opened", tag)
		body := fmt.Sprintf("Someone opened the link tagged %q.\n\nLanded on: %s%s\nDevice: %s\n\nWhat they went on to read, and for how long, is on the Readers screen:\n%s/console/readers\n\nYou are told once a day per tag.\n",
			tag, publicURL, page, device, publicURL)
		if err := m.Send(ctx, subject, body); err != nil {
			log.Error("tag-opened email", "tag", tag, "err", err)
		}
	}
}
