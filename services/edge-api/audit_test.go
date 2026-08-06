package main

import (
	"testing"
	"time"
)

func TestRetentionCutoffIsExactAndUTC(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.FixedZone("test", 3*60*60))
	cutoff := retentionCutoff(now, 90*24*time.Hour)
	want := now.UTC().Add(-90 * 24 * time.Hour)
	if !cutoff.Equal(want) || cutoff.Location() != time.UTC {
		t.Fatalf("cutoff=%v want=%v UTC", cutoff, want)
	}
}
