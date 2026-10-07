// Package metrics counts what Lighthouse does and sends the figures somewhere outside itself.
//
// Lighthouse watches other things, and can't report its own outage. Pushing a few numbers to an
// outside metrics service at the end of every round of checks fixes that: when the numbers stop
// arriving, the service raises the alarm (a "dead man's switch"). The same numbers feed
// dashboards and an alert on server errors.
//
// On Cloud Run nothing can scrape an instance that sleeps, so the figures are pushed. They are
// sent as InfluxDB line protocol, which Grafana Cloud's Prometheus accepts over plain HTTP: each
// "measurement field" becomes the Prometheus series measurement_field.
package metrics

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sample is one line: a measurement, its labels and its numeric fields.
type Sample struct {
	Name   string
	Labels map[string]string
	Fields map[string]float64
}

// Registry counts requests by status class between pushes.
type Registry struct {
	mu       sync.Mutex
	requests map[string]int
}

func NewRegistry() *Registry { return &Registry{requests: map[string]int{}} }

// Request counts one answered request.
func (r *Registry) Request(status int) {
	if r == nil {
		return
	}
	class := fmt.Sprintf("%dxx", status/100)
	r.mu.Lock()
	r.requests[class]++
	r.mu.Unlock()
}

// Drain returns the requests counted since the last call, every class present (zero included, so
// a series exists to alert on before the first error), and starts counting again.
func (r *Registry) Drain() []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Sample
	for _, class := range []string{"2xx", "3xx", "4xx", "5xx"} {
		out = append(out, Sample{Name: "lighthouse_http", Labels: map[string]string{"class": class},
			Fields: map[string]float64{"requests": float64(r.requests[class])}})
	}
	r.requests = map[string]int{}
	return out
}

// Pusher sends samples to an InfluxDB line-protocol endpoint with basic authentication.
type Pusher struct {
	URL, User, Token string
	Client           *http.Client
}

// Push sends the samples, all stamped with at. Nothing is sent when no URL is configured.
func (p *Pusher) Push(ctx context.Context, samples []Sample, at time.Time) error {
	if p == nil || p.URL == "" || len(samples) == 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader([]byte(Encode(samples, at))))
	if err != nil {
		return err
	}
	req.SetBasicAuth(p.User, p.Token)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("metrics push: the endpoint answered %d", resp.StatusCode)
	}
	return nil
}

var escaper = strings.NewReplacer(`\`, `\\`, ",", `\,`, " ", `\ `, "=", `\=`, "\n", " ")

// Encode writes samples as line protocol: name,label=value field=1.5 <unix nanoseconds>.
// Labels and fields are sorted, so the output is stable.
func Encode(samples []Sample, at time.Time) string {
	var b strings.Builder
	for _, s := range samples {
		if len(s.Fields) == 0 {
			continue
		}
		b.WriteString(escaper.Replace(s.Name))
		for _, k := range sorted(s.Labels) {
			if s.Labels[k] == "" {
				continue
			}
			fmt.Fprintf(&b, ",%s=%s", escaper.Replace(k), escaper.Replace(s.Labels[k]))
		}
		for i, k := range sorted(s.Fields) {
			sep := ","
			if i == 0 {
				sep = " "
			}
			fmt.Fprintf(&b, "%s%s=%g", sep, escaper.Replace(k), s.Fields[k])
		}
		fmt.Fprintf(&b, " %d\n", at.UnixNano())
	}
	return b.String()
}

func sorted[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
