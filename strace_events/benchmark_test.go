package strace_events_test

import (
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/strace_events"
)

// corpusParts is the in-repo corpus as two traces' lines, measured apart since a line's cost grows
// with its length: the fixtures in testdata, in a fixed order, and the few long lines longLines makes,
// whose bytes outnumber all the fixtures' together.
func corpusParts(benchmark *testing.B) map[string][]string {
	benchmark.Helper()
	corpus := inRepositoryCorpus(benchmark)
	names := make([]string, 0, len(corpus))
	for name := range corpus {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := map[string][]string{}
	for _, name := range names {
		part := "fixtures"
		if name == "long lines" {
			part = "long lines"
		}
		parts[part] = append(parts[part], strings.Split(strings.TrimSuffix(corpus[name], "\n"), "\n")...)
	}
	return parts
}

// measureEachPart runs read over each part of the in-repo corpus as a benchmark of its own.
func measureEachPart(benchmark *testing.B, read func(lines []string)) {
	parts := corpusParts(benchmark)
	for _, part := range []string{"fixtures", "long lines"} {
		benchmark.Run(part, func(benchmark *testing.B) {
			measurePerLine(benchmark, parts[part], read)
		})
	}
}

// measurePerLine runs read over lines benchmark.N times and reports ns/line and allocs/line beside
// the per-pass figures, since a pass is the whole corpus and a line is what a trace is paid by.
func measurePerLine(benchmark *testing.B, lines []string, read func(lines []string)) {
	benchmark.ReportAllocs()
	var before runtime.MemStats
	var after runtime.MemStats
	runtime.ReadMemStats(&before)
	benchmark.ResetTimer()
	for range benchmark.N {
		read(lines)
	}
	benchmark.StopTimer()
	runtime.ReadMemStats(&after)
	linesRead := float64(benchmark.N * len(lines))
	benchmark.ReportMetric(float64(benchmark.Elapsed().Nanoseconds())/linesRead, "ns/line")
	benchmark.ReportMetric(float64(after.Mallocs-before.Mallocs)/linesRead, "allocs/line")
}

// Loom's regular expressions over the in-repo corpus, each line read as planner/trace.go reads it: the
// call it finishes, then on each call the descriptor its AT_FDCWD check reads, the path argument after
// it, and the opened path.
func BenchmarkLoomsRegularExpressions(benchmark *testing.B) {
	measureEachPart(benchmark, func(lines []string) {
		parser := &loomParser{}
		for _, text := range lines {
			event, isEvent := parser.line(text)
			if !isEvent {
				continue
			}
			arguments := event.arguments
			if match := loomDescriptor.FindStringSubmatch(arguments); match != nil {
				arguments = arguments[len(match[0]):]
			}
			// Ignored: the benchmark measures the read, and the differential judges what it read.
			_, _, _ = loomQuoted(arguments)
			_ = loomOpenedPath.FindStringSubmatch(event.line)
		}
	})
}

// This package over the same corpus, each line read to the same parts: the call it finishes, its
// descriptor, the path argument after it, and the opened path, which the call already holds.
func BenchmarkStraceEvents(benchmark *testing.B) {
	measureEachPart(benchmark, func(lines []string) {
		joiner := &strace_events.Joiner{}
		for _, text := range lines {
			call, isCall := joiner.Join(strace_events.ParseLine(text))
			if !isCall {
				continue
			}
			arguments := call.Arguments
			if _, rest, found := strace_events.DescriptorArgument(arguments); found {
				arguments = rest
			}
			_, _, _ = strace_events.QuotedArgument(arguments)
		}
	})
}
