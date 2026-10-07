package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEncode(t *testing.T) {
	at := time.Unix(1700000000, 0)
	got := Encode([]Sample{
		{Name: "lighthouse_monitor", Labels: map[string]string{"monitor": "ghost chat", "empty": ""}, Fields: map[string]float64{"up": 1, "latency_ms": 41.5}},
		{Name: "lighthouse_tick", Fields: map[string]float64{"checks": 3}},
		{Name: "nothing"},
	}, at)
	want := "lighthouse_monitor,monitor=ghost\\ chat latency_ms=41.5,up=1 1700000000000000000\n" +
		"lighthouse_tick checks=3 1700000000000000000\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRegistryCountsByClassAndStartsAgain(t *testing.T) {
	r := NewRegistry()
	for _, status := range []int{200, 204, 302, 404, 500, 503} {
		r.Request(status)
	}
	counts := map[string]float64{}
	for _, s := range r.Drain() {
		counts[s.Labels["class"]] = s.Fields["requests"]
	}
	if counts["2xx"] != 2 || counts["3xx"] != 1 || counts["4xx"] != 1 || counts["5xx"] != 2 {
		t.Fatalf("counts: %v", counts)
	}
	for _, s := range r.Drain() {
		if s.Fields["requests"] != 0 {
			t.Fatalf("after draining, counting starts again: %+v", s)
		}
	}
	var none *Registry
	none.Request(200) // a server without metrics doesn't count, and doesn't crash
}

func TestPush(t *testing.T) {
	var body, user, pass string
	status := http.StatusNoContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		user, pass, _ = r.BasicAuth()
		w.WriteHeader(status)
	}))
	defer srv.Close()
	p := &Pusher{URL: srv.URL, User: "123", Token: "secret"}
	samples := []Sample{{Name: "lighthouse_tick", Fields: map[string]float64{"checks": 3}}}
	if err := p.Push(context.Background(), samples, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if body != "lighthouse_tick checks=3 1000000000\n" || user != "123" || pass != "secret" {
		t.Fatalf("sent %q as %s:%s", body, user, pass)
	}
	status = http.StatusUnauthorized
	if err := p.Push(context.Background(), samples, time.Unix(1, 0)); err == nil {
		t.Fatal("a refused push is an error")
	}
	if err := (&Pusher{}).Push(context.Background(), samples, time.Unix(1, 0)); err != nil {
		t.Fatalf("with no endpoint configured, nothing is sent: %v", err)
	}
}
