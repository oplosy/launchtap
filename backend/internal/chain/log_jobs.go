package chain

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

// MaxLogFetchConcurrency bounds WithLogConcurrency.
const MaxLogFetchConcurrency = 16

// logJob fetches one independent part of a discovery range.
type logJob func(context.Context) ([]types.Log, error)

// WithLogConcurrency lets discovery keep up to limit eth_getLogs calls in flight (pre-flight
// 2026-11-13-parallel-log-fetch). One keeps the sequential path. Call it before the first
// Discover; the discoverer is not safe for concurrent Discover calls.
func (d *Discoverer) WithLogConcurrency(limit int) (*Discoverer, error) {
	if limit < 1 || limit > MaxLogFetchConcurrency {
		return nil, errors.New("log fetch concurrency is invalid")
	}
	if limit == 1 {
		d.slots = nil
	} else {
		d.slots = make(chan struct{}, limit)
	}
	return d, nil
}

// filterLogs runs one eth_getLogs call, holding a concurrency slot when parallel fetching is on.
func (d *Discoverer) filterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	if d.slots != nil {
		select {
		case d.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer func() { <-d.slots }()
	}
	return d.source.FilterLogs(ctx, query)
}

// runLogJobs runs jobs and returns each job's logs in job order. Without parallel fetching they
// run one after another and stop at the first error; with it they run concurrently, the first
// failure cancels the rest, and that failure is returned.
func (d *Discoverer) runLogJobs(ctx context.Context, jobs []logJob) ([][]types.Log, error) {
	results := make([][]types.Log, len(jobs))
	if d.slots == nil || len(jobs) <= 1 {
		for i, job := range jobs {
			logs, err := job(ctx)
			if err != nil {
				return nil, err
			}
			results[i] = logs
		}
		return results, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		group sync.WaitGroup
		once  sync.Once
		first error
	)
	for i, job := range jobs {
		group.Go(func() {
			logs, err := job(ctx)
			if err != nil {
				once.Do(func() {
					first = err
					cancel()
				})
				return
			}
			results[i] = logs
		})
	}
	group.Wait()
	if first != nil {
		return nil, first
	}
	return results, nil
}

// concatLogs flattens per-job results in job order.
func concatLogs(results [][]types.Log) []types.Log {
	return slices.Concat(results...)
}
