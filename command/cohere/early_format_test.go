package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/types/program"
)

// The early format pass reads a file before the graph build does (#g3046x5). A file edited between the two
// reads must not keep the early result: the program holds the new bytes, so the result describes a file that
// no longer exists. Here the edit lands for real on disk between the speculation and the build, and the control
// is the same file left alone, without which a speculation that kept nothing would pass.
func TestAnEarlyFormatResultIsDiscardedWhenTheFileChangesBeforeTheBuild(test *testing.T) {
	test.Parallel()
	for _, edited := range []bool{false, true} {
		test.Run(fmt.Sprintf("edited=%t", edited), func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			fileName := filepath.Join(directory, "A.ts")
			if writeError := os.WriteFile(fileName, []byte("export const a = 1;\n"), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			unchanged := func(_ string, text string, _ *ast.SourceFile) (string, error) { return text, nil }
			speculation := speculateFormatOn([]string{fileName}, unchanged, 1, 1, true, nil)
			speculation.wait()
			if _, attempted := speculation.attempts[fileName]; !attempted {
				test.Fatal("the speculation attempted nothing, so nothing below is about keeping it")
			}

			if edited {
				if writeError := os.WriteFile(fileName, []byte("export const a = 2;\n"), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			configuration := `{"compilerOptions":{"strict":true,"module":"esnext","target":"esnext","moduleResolution":"bundler"},"include":["**/*.ts"]}`
			if writeError := os.WriteFile(
				filepath.Join(directory, "tsconfig.json"),
				[]byte(configuration),
				0o644,
			); writeError != nil {
				test.Fatal(writeError)
			}
			graph, buildError := program.Build(
				program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory},
			)
			if buildError != nil {
				test.Fatal(buildError)
			}

			_, kept := speculation.keepable(nil, graph)[fileName]
			if kept == edited {
				test.Errorf(
					"edited between the early read and the build: %t, and the early result was kept: %t",
					edited,
					kept,
				)
			}
		})
	}
}

// The early pass runs on every core until the walk starts, then narrows to its usual share: the workers past the
// share finish the file they are on and take no more, and the files they leave go to the workers that remain,
// so every file is still attempted. Measured by how many transforms run at once after the narrowing.
func TestANarrowedSpeculationRunsOnFewerWorkersAndStillReachesEveryFile(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	var candidates []string
	for index := range 40 {
		fileName := filepath.Join(directory, fmt.Sprintf("f%02d.md", index))
		if writeError := os.WriteFile(fileName, []byte("# Title\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		candidates = append(candidates, fileName)
	}
	const workers = 8
	const narrowed = 2

	// Each transform begun before the narrowing holds its file until the test lets it go, so every worker is
	// mid-file when it lands. Each begun after it holds for a moment, so any two running at once would overlap,
	// and counts how many ran at once.
	started := make(chan struct{}, len(candidates))
	release := make(chan struct{})
	var isNarrowed atomic.Bool
	var runningAfter atomic.Int32
	var mostAfter atomic.Int32
	holding := func(_ string, text string, _ *ast.SourceFile) (string, error) {
		if !isNarrowed.Load() {
			started <- struct{}{}
			<-release
			return text, nil
		}
		now := runningAfter.Add(1)
		defer runningAfter.Add(-1)
		for most := mostAfter.Load(); now > most && !mostAfter.CompareAndSwap(most, now); most = mostAfter.Load() {
		}
		time.Sleep(2 * time.Millisecond)
		return text, nil
	}

	speculation := speculateFormatOn(candidates, holding, 1, workers, true, nil)
	for range workers {
		<-started
	}
	speculation.narrow(narrowed)
	isNarrowed.Store(true)
	close(release)
	speculation.wait()

	if most := mostAfter.Load(); most == 0 || most > narrowed {
		test.Errorf(
			"after narrowing to %d workers, %d files were formatted at once (0 means none was begun after it)",
			narrowed,
			most,
		)
	}
	if len(speculation.attempts) != len(candidates) {
		test.Errorf(
			"narrowed, the speculation kept %d of %d unchanged files: files the narrowed workers left were lost",
			len(speculation.attempts),
			len(candidates),
		)
	}
}
