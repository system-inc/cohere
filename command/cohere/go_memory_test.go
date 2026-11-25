package main

import (
	"encoding/json"
	"testing"
)

func TestGoMemoryLine(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name         string
		record       string
		buildsAtOnce int
		want         string
	}{
		{
			name:         "half of what is available on Monkee, with the builds it ran at once",
			record:       `{"limitBytes":5750000000,"source":"Available","availableBytes":11500000000}`,
			buildsAtOnce: 3,
			want:         "memory limit 5.4 GiB, half of the 11.5 GB available: 3 builds at once",
		},
		{
			name:   "half of what is available on a run of one build",
			record: `{"limitBytes":125000000000,"source":"Available","availableBytes":250000000000}`,
			want:   "memory limit 116.4 GiB, half of the 250.0 GB available",
		},
		{
			name:         "one build at once",
			record:       `{"limitBytes":2000000000,"source":"Available","availableBytes":4000000000}`,
			buildsAtOnce: 1,
			want:         "memory limit 1.9 GiB, half of the 4.0 GB available: 1 build at once",
		},
		{
			name: "the user's environment with a limit",
			record: `{"limitBytes":21474836480,"source":"Environment","availableBytes":0,` +
				`"environment":"GOMEMLIMIT=20GiB"}`,
			want: "memory limit 20.0 GiB, set by the environment (GOMEMLIMIT=20GiB)",
		},
		{
			name:   "the user's environment without one",
			record: `{"limitBytes":0,"source":"Environment","availableBytes":0,"environment":"GOGC=100"}`,
			want:   "memory limit none, since the environment sets cohere-go's collector (GOGC=100)",
		},
		{
			name:   "available memory unreadable",
			record: `{"limitBytes":8589934592,"source":"Unreadable","availableBytes":0}`,
			want: "memory limit 8.0 GiB as a bound alone, with the collector at Go's default, since available " +
				"memory could not be read",
		},
		{
			name:   "a source this front door does not know",
			record: `{"limitBytes":4294967296,"source":"Tomorrow","availableBytes":0}`,
			want:   `memory limit 4.0 GiB, from a source this front door does not know ("Tomorrow")`,
		},
	}
	for _, described := range cases {
		test.Run(described.name, func(test *testing.T) {
			test.Parallel()
			var memory goMemory
			if decodeError := json.Unmarshal([]byte(described.record), &memory); decodeError != nil {
				test.Fatalf("decoding %s: %v", described.record, decodeError)
			}
			if got := memory.line(described.buildsAtOnce); got != described.want {
				test.Errorf("line(%d) = %q, want %q", described.buildsAtOnce, got, described.want)
			}
		})
	}
}
