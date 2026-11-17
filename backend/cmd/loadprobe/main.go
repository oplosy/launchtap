// Command loadprobe measures read latency of a running API. It sends GET requests to the given
// paths from a fixed number of concurrent workers for a fixed duration and prints, per path, the
// request count, error count and p50/p95/p99 latency. It never writes and never authenticates, so
// it is safe to point at any deployment you are allowed to load; mind that deployment's rate limits
// (a 429 counts as an error).
//
//	go run ./cmd/loadprobe -base http://127.0.0.1:8080/v1 -c 16 -d 30s /tokens /tokens/highlights
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

type sample struct {
	path    string
	elapsed time.Duration
	failed  bool
}

type summary struct {
	Path          string
	Requests      int
	Errors        int
	P50, P95, P99 time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "loadprobe:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("loadprobe", flag.ContinueOnError)
	base := flags.String("base", "http://127.0.0.1:8080/v1", "API base URL")
	workers := flags.Int("c", 8, "concurrent workers")
	duration := flags.Duration("d", 20*time.Second, "test duration")
	timeout := flags.Duration("timeout", 10*time.Second, "per-request timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths := flags.Args()
	if len(paths) == 0 {
		return errors.New("at least one path is required")
	}
	if *workers < 1 || *workers > 1024 {
		return errors.New("-c must be between 1 and 1024")
	}
	if *duration <= 0 || *timeout <= 0 {
		return errors.New("-d and -timeout must be positive")
	}
	baseURL, err := url.Parse(strings.TrimRight(*base, "/"))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return fmt.Errorf("invalid -base %q", *base)
	}
	targets := make([]string, len(paths))
	for i, path := range paths {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("path %q must start with /", path)
		}
		targets[i] = baseURL.String() + path
	}

	client := &http.Client{Timeout: *timeout, Transport: &http.Transport{MaxIdleConnsPerHost: *workers}}
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	results := make(chan sample, 1024)
	var group sync.WaitGroup
	for worker := range *workers {
		group.Go(func() {
			for i := worker; ctx.Err() == nil; i++ {
				index := i % len(targets)
				results <- probe(ctx, client, paths[index], targets[index])
			}
		})
	}
	go func() { group.Wait(); close(results) }()
	samples := make([]sample, 0, 4096)
	for result := range results {
		samples = append(samples, result)
	}
	return report(out, summarize(paths, samples), *workers, *duration)
}

// probe sends one request. A request cut short by the end of the run is dropped rather than
// counted as an error.
func probe(ctx context.Context, client *http.Client, path, target string) sample {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return sample{path: path, failed: true}
	}
	started := time.Now()
	response, err := client.Do(request)
	if err == nil {
		_, err = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	elapsed := time.Since(started)
	if ctx.Err() != nil {
		return sample{path: "", elapsed: elapsed}
	}
	return sample{path: path, elapsed: elapsed, failed: err != nil || response.StatusCode >= 400}
}

func summarize(paths []string, samples []sample) []summary {
	byPath := make(map[string][]time.Duration, len(paths))
	errorsByPath := make(map[string]int, len(paths))
	for _, s := range samples {
		if s.path == "" {
			continue
		}
		if s.failed {
			errorsByPath[s.path]++
			continue
		}
		byPath[s.path] = append(byPath[s.path], s.elapsed)
	}
	out := make([]summary, 0, len(paths))
	for _, path := range slices.Compact(slices.Clone(paths)) {
		durations := byPath[path]
		slices.Sort(durations)
		out = append(out, summary{
			Path: path, Requests: len(durations) + errorsByPath[path], Errors: errorsByPath[path],
			P50: percentile(durations, 50), P95: percentile(durations, 95), P99: percentile(durations, 99),
		})
	}
	return out
}

// percentile returns the nearest-rank percentile of sorted durations, or zero when empty.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100
	return sorted[max(rank, 1)-1]
}

// report prints the run header and one row per path. The tabwriter buffers the rows and returns
// the first write error from Flush.
func report(out io.Writer, rows []summary, workers int, duration time.Duration) error {
	if _, err := fmt.Fprintf(out, "workers=%d duration=%s\n", workers, duration); err != nil {
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "path\trequests\terrors\tp50\tp95\tp99")
	for _, row := range rows {
		_, _ = fmt.Fprintf(table, "%s\t%d\t%d\t%s\t%s\t%s\n", row.Path, row.Requests, row.Errors,
			row.P50.Round(10*time.Microsecond), row.P95.Round(10*time.Microsecond), row.P99.Round(10*time.Microsecond))
	}
	return table.Flush()
}
