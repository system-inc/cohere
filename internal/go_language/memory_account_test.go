package go_language

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/contract"
)

// monkeeAvailableBytes is the memory Monkee, 16 GB, read as available in #jrr6a5x's rows, and monkeeLimitBytes the half
// of it the front door's policy sets as the limit.
const (
	monkeeAvailableBytes uint64 = 11_500_000_000
	monkeeLimitBytes     int64  = 5_750_000_000
)

// largeAvailableBytes is a 250 GB machine's available memory, and largeLimitBytes the half of it the policy sets.
const (
	largeAvailableBytes uint64 = 250_000_000_000
	largeLimitBytes     int64  = 125_000_000_000
)

// eightGibibytes is the limit the width's heaps were measured under, on machines whose half of available passed it.
const eightGibibytes int64 = 8 << 30

// halfOfAvailable is a decision whose limit, limitBytes, is half the memory available, as the policy sets it.
func halfOfAvailable(limitBytes int64) *contract.Memory {
	return &contract.Memory{
		LimitBytes:     limitBytes,
		Source:         contract.MemorySourceAvailable,
		AvailableBytes: uint64(limitBytes) * 2,
	}
}

func TestDecideMemory(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name        string
		environment map[string]string
		current     int64
		available   uint64
		// share is how many project children divide the half, 1 for a run alone.
		share uint64
		// set is every limit decideMemory set, the read with -1 left out.
		set  []int64
		want contract.Memory
	}{
		{
			name:    "Monkee's half of available is the limit, set by the policy and left alone",
			current: monkeeLimitBytes, available: monkeeAvailableBytes,
			want: contract.Memory{
				LimitBytes: monkeeLimitBytes, Source: contract.MemorySourceAvailable,
				AvailableBytes: monkeeAvailableBytes,
			},
		},
		{
			name:    "one of 4 project children at once takes a quarter of Monkee's half, and says so",
			current: monkeeLimitBytes / 4, available: monkeeAvailableBytes, share: 4,
			want: contract.Memory{
				LimitBytes: monkeeLimitBytes / 4, Source: contract.MemorySourceAvailable,
				AvailableBytes: monkeeAvailableBytes, SharedBy: 4,
			},
		},
		{
			name:    "a 250 GB machine's half of available is the limit, never lowered",
			current: largeLimitBytes, available: largeAvailableBytes,
			want: contract.Memory{
				LimitBytes: largeLimitBytes, Source: contract.MemorySourceAvailable,
				AvailableBytes: largeAvailableBytes,
			},
		},
		{
			name: "no limit, from memory the policy could not read, gets the bound", current: math.MaxInt64,
			set: []int64{unreadableMemoryBound},
			want: contract.Memory{
				LimitBytes: unreadableMemoryBound, Source: contract.MemorySourceUnreadable,
			},
		},
		{
			name: "GOGC in the environment wins", environment: map[string]string{"GOGC": "100"}, current: math.MaxInt64,
			available: largeAvailableBytes,
			want: contract.Memory{
				Source: contract.MemorySourceEnvironment, Environment: "GOGC=100",
			},
		},
		{
			name: "GOMEMLIMIT in the environment wins", environment: map[string]string{"GOMEMLIMIT": "20GiB"},
			current: 20 << 30, available: largeAvailableBytes,
			want: contract.Memory{
				LimitBytes: 20 << 30, Source: contract.MemorySourceEnvironment, Environment: "GOMEMLIMIT=20GiB",
			},
		},
		{
			name: "a variable set empty still wins", environment: map[string]string{"GOGC": ""}, current: math.MaxInt64,
			want: contract.Memory{
				Source: contract.MemorySourceEnvironment, Environment: "GOGC=",
			},
		},
	}
	for _, decided := range cases {
		test.Run(decided.name, func(test *testing.T) {
			test.Parallel()
			lookupEnvironment := func(name string) (string, bool) {
				value, set := decided.environment[name]
				return value, set
			}
			limit := decided.current
			var set []int64
			setMemoryLimit := func(requested int64) int64 {
				previous := limit
				if requested >= 0 {
					set = append(set, requested)
					limit = requested
				}
				return previous
			}

			memory := decideMemory(lookupEnvironment, setMemoryLimit, decided.available, max(decided.share, 1))

			if !slices.Equal(set, decided.set) {
				test.Errorf("set the limit to %v, want %v", set, decided.set)
			}
			if memory != decided.want {
				test.Errorf("decided %+v, want %+v", memory, decided.want)
			}
		})
	}
}

// runtimeDebugPath is runtime/debug's import path, whose SetMemoryLimit only Main may name.
const runtimeDebugPath = "runtime/debug"

// The limit is set once, in Main, before the run starts, and nothing in cohere-go sets it after: the limit is the
// ceiling the front door's policy set, half the memory available, and a raise past it is never allowed (#jrr6a5x).
// decideMemory, which Main hands debug.SetMemoryLimit, sets one only where the policy set none (TestDecideMemory).
// Read from the package's source, not its tests.
func TestOnlyMainSetsTheMemoryLimit(test *testing.T) {
	test.Parallel()
	files, globError := filepath.Glob("*.go")
	if globError != nil {
		test.Fatalf("listing the package's files: %v", globError)
	}
	fileSet := token.NewFileSet()
	named := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, parseError := parser.ParseFile(fileSet, file, nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", file, parseError)
		}
		name := importedAs(parsed, runtimeDebugPath)
		if name == "" {
			continue
		}
		for _, declaration := range parsed.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			allowed := isFunction && function.Name.Name == "Main" && function.Recv == nil
			ast.Inspect(declaration, func(node ast.Node) bool {
				selector, isSelector := node.(*ast.SelectorExpr)
				if !isSelector || selector.Sel.Name != "SetMemoryLimit" {
					return true
				}
				if identifier, isIdentifier := selector.X.(*ast.Ident); !isIdentifier || identifier.Name != name {
					return true
				}
				named++
				if !allowed {
					test.Errorf("%s names debug.SetMemoryLimit outside Main, so the run's limit could move after it "+
						"started; the limit is the policy's ceiling, decided once (decideMemory)",
						fileSet.Position(selector.Pos()))
				}
				return true
			})
		}
	}
	if named != 1 {
		test.Errorf("debug.SetMemoryLimit is named %d times in the package, want once, in Main", named)
	}
}
