package go_engine

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// The duplicates report never moves a verdict: the same tree, once with a finding and once clean, gives the same
// exit code and the same stream with `--duplicates` as without it, but for the one duplicates record, and the summary's
// findings and completeness are the same. Each run is on its own copy, so neither reads the other's cache.
func TestTheDuplicatesReportNeverMovesTheVerdict(test *testing.T) {
	test.Parallel()
	for _, fixture := range []string{"duplicates", "vet-finding"} {
		test.Run(fixture, func(test *testing.T) {
			test.Parallel()
			check := func(extra ...string) (string, int) {
				root := copyFixture(test, filepath.Join("testdata", fixture))
				arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"},
					extra...)
				var standardOutput bytes.Buffer
				var standardError bytes.Buffer
				exitCode := run(arguments, &standardOutput, &standardError, rules.All(), nil)
				return normalize(standardOutput.String(), root), exitCode
			}
			without, withoutExit := check()
			with, withExit := check("--duplicates")
			if withExit != withoutExit {
				test.Fatalf("exit %d with --duplicates and %d without", withExit, withoutExit)
			}
			var kept []string
			duplicatesRecords := 0
			for _, line := range strings.Split(with, "\n") {
				if strings.HasPrefix(line, `{"kind":"duplicates"`) {
					duplicatesRecords++
					continue
				}
				kept = append(kept, line)
			}
			if duplicatesRecords != 1 {
				test.Errorf("--duplicates wrote %d duplicates records, where one was expected", duplicatesRecords)
			}
			if strings.Join(kept, "\n") != without {
				test.Errorf("the stream differs beyond the duplicates record:\nwith:\n%s\nwithout:\n%s", with, without)
			}
		})
	}
}
