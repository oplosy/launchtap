package main

import (
	"testing"
)

func TestParseFlagsValidation(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-rate=0"}, {"-batch=0"}, {"-batch=101"}, {"-rate=-1"}} {
		if _, err := parseFlags(args); err == nil {
			t.Fatalf("parseFlags(%v) accepted invalid input", args)
		}
	}
	got, err := parseFlags(nil)
	if err != nil || got.Rate != 5 || got.Batch != 50 || got.DryRun || got.SeedPositions {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	seed, err := parseFlags([]string{"-seed-positions", "-dry-run"})
	if err != nil || !seed.SeedPositions || !seed.DryRun {
		t.Fatalf("seed flags = %+v, %v", seed, err)
	}
}

func TestParseFlagsRejectsUnusableRate(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{"-rate=Inf", "-rate=NaN", "-rate=+Inf", "-rate=0.0001"} {
		if _, err := parseFlags([]string{arg}); err == nil {
			t.Fatalf("parseFlags(%s) accepted invalid rate", arg)
		}
	}
}
