package main

import (
	"encoding/json"
	"testing"
)

func TestEngineMemoryLine(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name   string
		record string
		want   string
	}{
		{
			name:   "the cap",
			record: `{"limitBytes":8589934592,"capBytes":8589934592,"source":"Cap","availableBytes":44023414784}`,
			want:   "the Go engine's limit is its 8.6 GB cap, under half of the 44 GB available",
		},
		{
			name:   "half of what is available",
			record: `{"limitBytes":6000000000,"capBytes":8589934592,"source":"Available","availableBytes":12000000000}`,
			want:   "the Go engine's limit is 6.0 GB, half of the 12 GB available, under its 8.6 GB cap",
		},
		{
			name: "the user's environment",
			record: `{"limitBytes":0,"capBytes":8589934592,"source":"Environment","availableBytes":0,` +
				`"environment":"GOMEMLIMIT=20GiB"}`,
			want: "the environment sets the Go engine's collector (GOMEMLIMIT=20GiB), so its 8.6 GB cap does not apply",
		},
		{
			name:   "available memory unreadable",
			record: `{"limitBytes":8589934592,"capBytes":8589934592,"source":"Unreadable","availableBytes":0}`,
			want: "the Go engine's limit is its 8.6 GB cap, with the collector at Go's default, since available " +
				"memory could not be read",
		},
	}
	for _, described := range cases {
		test.Run(described.name, func(test *testing.T) {
			test.Parallel()
			var memory engineMemory
			if decodeError := json.Unmarshal([]byte(described.record), &memory); decodeError != nil {
				test.Fatalf("decoding %s: %v", described.record, decodeError)
			}
			if got := memory.line(); got != described.want {
				test.Errorf("line() = %q, want %q", got, described.want)
			}
		})
	}
}
