package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectRejectsEffectiveRaceGOFLAGS(t *testing.T) {
	for _, flags := range []string{"-race", "-race=true", "-race=1", "-race=t"} {
		t.Run(flags, func(t *testing.T) {
			root, policy, selected, args := setupCollectionFixture(t)
			args.baseline = "artifacts/perf/baseline"
			t.Setenv("GOFLAGS", flags)

			code, err := collectResults(policy, selected, args, root, "fixture")
			if err != nil || code != 2 {
				t.Fatalf("collectResults: code=%d err=%v", code, err)
			}
			if _, err := os.Stat(manifestPath(filepath.Join(root, args.candidate), "candidate")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("race collection published a candidate manifest: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, args.baseline)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("race collection created a baseline: %v", err)
			}
		})
	}
}
