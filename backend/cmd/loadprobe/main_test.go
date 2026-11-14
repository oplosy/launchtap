package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPercentileUsesNearestRank(t *testing.T) {
	t.Parallel()
	sorted := make([]time.Duration, 100)
	for i := range sorted {
		sorted[i] = time.Duration(i+1) * time.Millisecond
	}
	for _, tc := range []struct {
		p    int
		want time.Duration
	}{{50, 50 * time.Millisecond}, {95, 95 * time.Millisecond}, {99, 99 * time.Millisecond}, {100, 100 * time.Millisecond}} {
		if got := percentile(sorted, tc.p); got != tc.want {
			t.Errorf("p%d = %s, want %s", tc.p, got, tc.want)
		}
	}
	if got := percentile([]time.Duration{7}, 99); got != 7 {
		t.Errorf("single sample p99 = %s", got)
	}
	if got := percentile(nil, 95); got != 0 {
		t.Errorf("empty p95 = %s", got)
	}
}

func TestSummarizeCountsErrorsSeparately(t *testing.T) {
	t.Parallel()
	rows := summarize([]string{"/a", "/b"}, []sample{
		{path: "/a", elapsed: time.Millisecond}, {path: "/a", elapsed: 3 * time.Millisecond},
		{path: "/a", failed: true}, {path: ""}, {path: "/b", failed: true},
	})
	if rows[0].Requests != 3 || rows[0].Errors != 1 || rows[0].P99 != 3*time.Millisecond {
		t.Fatalf("/a = %+v", rows[0])
	}
	if rows[1].Requests != 1 || rows[1].Errors != 1 || rows[1].P95 != 0 {
		t.Fatalf("/b = %+v", rows[1])
	}
}

func TestRunReportsEveryPathAndCountsHTTPErrors(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.Path == "/v1/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := run([]string{"-base", server.URL + "/v1", "-c", "2", "-d", "200ms", "/ok", "/missing"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("output:\n%s", out.String())
	}
	ok, missing := strings.Fields(lines[2]), strings.Fields(lines[3])
	if ok[0] != "/ok" || ok[2] != "0" || ok[1] == "0" {
		t.Errorf("/ok row %v", ok)
	}
	if missing[0] != "/missing" || missing[1] != missing[2] {
		t.Errorf("/missing row %v: every request should be an error", missing)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{},
		{"-c", "0", "/a"},
		{"-base", "ftp://x", "/a"},
		{"no-slash"},
		{"-d", "0s", "/a"},
	} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Errorf("run(%v) accepted bad input", args)
		}
	}
}
