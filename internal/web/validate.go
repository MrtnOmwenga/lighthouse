package web

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/MrtnOmwenga/lighthouse/internal/auth"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,49}$`)

const maxSandboxMonitors = 10

// normalize fills defaults into a monitor and checks it. The database checks the same ranges;
// checking here too gives a useful message instead of a constraint name.
func normalize(in store.MonitorInput, id auth.Identity) (store.MonitorInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	defaults := map[*int]int{
		&in.IntervalSeconds: 60, &in.TimeoutMS: 10000, &in.ExpectedStatusMin: 200, &in.ExpectedStatusMax: 399,
		&in.FailureThreshold: 3, &in.RecoveryThreshold: 2,
	}
	for field, value := range defaults {
		if *field == 0 {
			*field = value
		}
	}
	if in.SimulatedMode == "" {
		in.SimulatedMode = "up"
	}
	if in.ExpectedText != nil && *in.ExpectedText == "" {
		in.ExpectedText = nil
	}

	// Every problem is reported, each against its field, so a form can mark them all at once.
	var bad []fieldError
	add := func(field, message string) { bad = append(bad, fieldError{field, message}) }
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		add("name", "1 to 100 characters.")
	}
	if !slugPattern.MatchString(in.Slug) {
		add("slug", "Lowercase letters, digits and dashes, up to 50.")
	}
	if in.Kind != "http" && in.Kind != "simulated" {
		add("kind", "http or simulated.")
	}
	if !oneOf(in.SimulatedMode, "up", "slow", "flaky", "down") {
		add("simulatedMode", "up, slow, flaky or down.")
	}
	if in.IntervalSeconds < 5 || in.IntervalSeconds > 3600 {
		add("intervalSeconds", "5 to 3600.")
	}
	if in.TimeoutMS < 100 || in.TimeoutMS > 30000 {
		add("timeoutMs", "100 to 30000.")
	}
	if in.ExpectedStatusMin < 100 || in.ExpectedStatusMax > 599 || in.ExpectedStatusMin > in.ExpectedStatusMax {
		add("expectedStatusMin", "A range within 100 to 599.")
	}
	if in.FailureThreshold < 1 || in.FailureThreshold > 20 {
		add("failureThreshold", "1 to 20.")
	}
	if in.RecoveryThreshold < 1 || in.RecoveryThreshold > 20 {
		add("recoveryThreshold", "1 to 20.")
	}
	if in.ExpectedText != nil && utf8.RuneCountInString(*in.ExpectedText) > 200 {
		add("expectedText", "Up to 200 characters.")
	}

	if in.Kind == "http" {
		if in.URL == nil {
			add("url", "Required for http monitors.")
		} else if u, err := url.Parse(strings.TrimSpace(*in.URL)); err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Hostname() == "" || u.User != nil || len(*in.URL) > 2000 {
			add("url", "An absolute http(s) URL without credentials.")
		} else {
			clean := u.String()
			in.URL = &clean
		}
	} else {
		in.URL = nil
	}

	// Sandboxes are for anyone on the internet: no real network traffic, no fast intervals.
	if !id.Owner() {
		if in.AllowPrivateNetwork {
			return in, errForbidden
		}
		if in.Kind == "http" {
			add("kind", "The sandbox only runs simulated monitors: it doesn't send traffic to real sites.")
		}
		if in.IntervalSeconds >= 5 && in.IntervalSeconds < 10 {
			add("intervalSeconds", "At least 10 in the sandbox.")
		}
	}
	if len(bad) > 0 {
		return in, invalidFields(bad)
	}
	return in, nil
}

func oneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}

func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 50 {
			break
		}
	}
	return strings.TrimRight(b.String(), "-")
}
