package stats

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkerRetriesFailedClaimAndHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wake := make(chan struct{}, 1)
	claim := Claim{ChainID: 4663, Token: [20]byte{1}, Generation: 7}
	source := &workerTestSource{claim: claim, cancel: cancel}
	workerErrors := make(chan error, 1)
	worker := Worker{
		Source:    source,
		WorkerID:  "worker-test",
		BatchSize: 1,
		Wake:      wake,
		OnError:   func(got Claim, err error) { workerErrors <- err },
	}

	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	select {
	case err := <-workerErrors:
		if !errors.Is(err, errTestCompute) {
			t.Fatalf("OnError received %v, want %v", err, errTestCompute)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not report the compute error")
	}
	wake <- struct{}{}

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
	if source.claimCalls != 2 || source.computeCalls != 2 || source.completeCalls != 1 {
		t.Fatalf("calls: claim=%d compute=%d complete=%d; want 2, 2, 1",
			source.claimCalls, source.computeCalls, source.completeCalls)
	}
}

func TestWorkerBatchComputerKeepsFailedClaimsDirty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claims := []Claim{
		{ChainID: 4663, Token: [20]byte{1}, Generation: 1},
		{ChainID: 4663, Token: [20]byte{2}, Generation: 1},
		{ChainID: 4663, Token: [20]byte{3}, Generation: 1},
	}
	source := &batchWorkerTestSource{claims: claims, cancel: cancel}
	var reported []Claim
	worker := Worker{
		Source:   source,
		WorkerID: "worker-batch-test",
		OnError: func(claim Claim, err error) {
			if !errors.Is(err, errTestBatchCompute) {
				t.Errorf("OnError received %v, want %v", err, errTestBatchCompute)
			}
			reported = append(reported, claim)
		},
	}

	err := worker.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context cancellation", err)
	}
	if source.batchCalls != 1 || source.computeCalls != 0 {
		t.Fatalf("batch/individual compute calls = %d/%d; want 1/0", source.batchCalls, source.computeCalls)
	}
	if len(reported) != 1 || reported[0] != claims[1] {
		t.Fatalf("reported claims = %#v, want only %#v", reported, claims[1])
	}
	if len(source.completed) != 2 || source.completed[0] != claims[0] || source.completed[1] != claims[2] {
		t.Fatalf("completed claims = %#v, want first and third only", source.completed)
	}
}

var errTestCompute = errors.New("test compute failure")
var errTestBatchCompute = errors.New("test batch compute failure")

type workerTestSource struct {
	claim         Claim
	cancel        context.CancelFunc
	claimCalls    int
	computeCalls  int
	completeCalls int
}

func (source *workerTestSource) Claim(context.Context, string, int32) ([]Claim, error) {
	source.claimCalls++
	return []Claim{source.claim}, nil
}

func (source *workerTestSource) Compute(context.Context, Claim) error {
	source.computeCalls++
	if source.computeCalls == 1 {
		return errTestCompute
	}
	return nil
}

func (source *workerTestSource) Complete(context.Context, Claim, string) (bool, error) {
	source.completeCalls++
	source.cancel()
	return true, nil
}

type batchWorkerTestSource struct {
	claims       []Claim
	cancel       context.CancelFunc
	batchCalls   int
	computeCalls int
	completed    []Claim
}

func (source *batchWorkerTestSource) Claim(context.Context, string, int32) ([]Claim, error) {
	return source.claims, nil
}

func (source *batchWorkerTestSource) Compute(context.Context, Claim) error {
	source.computeCalls++
	return nil
}

func (source *batchWorkerTestSource) ComputeBatch(_ context.Context, claims []Claim) []error {
	source.batchCalls++
	if len(claims) != 3 {
		return nil
	}
	return []error{nil, errTestBatchCompute, nil}
}

func (source *batchWorkerTestSource) Complete(_ context.Context, claim Claim, _ string) (bool, error) {
	source.completed = append(source.completed, claim)
	if len(source.completed) == 2 {
		source.cancel()
	}
	return true, nil
}

func TestWorkerReportsComputedCount(t *testing.T) {
	claims := []Claim{{ChainID: 1, Generation: 1}, {ChainID: 1, Token: [20]byte{2}, Generation: 1}, {ChainID: 1, Token: [20]byte{3}, Generation: 1}}
	source := &countingSource{claims: claims, failAt: 1}
	var reported []int
	worker := Worker{Source: source, WorkerID: "count-test", OnComputed: func(count int) { reported = append(reported, count) }}
	if _, err := worker.drain(context.Background(), 32); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0] != 2 {
		t.Fatalf("OnComputed calls = %v, want [2]", reported)
	}
}

type countingSource struct {
	claims []Claim
	failAt int
	calls  int
}

func (s *countingSource) Claim(context.Context, string, int32) ([]Claim, error) { return s.claims, nil }
func (s *countingSource) Compute(context.Context, Claim) error {
	s.calls++
	if s.calls-1 == s.failAt {
		return errTestCompute
	}
	return nil
}
func (s *countingSource) Complete(context.Context, Claim, string) (bool, error) { return true, nil }
