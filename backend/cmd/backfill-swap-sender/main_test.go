package main

import "testing"

func TestParseFlagsValidation(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-rate=0"}, {"-batch=0"}, {"-batch=101"}, {"-rate=-1"}} {
		if _, err := parseFlags(args); err == nil {
			t.Fatalf("parseFlags(%v) accepted invalid input", args)
		}
	}
	got, err := parseFlags(nil)
	if err != nil || got.Rate != 5 || got.Batch != 50 || got.DryRun {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
}
