package printf

import (
	"go/token"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"
)

// holdsObjects are functions and a variable of each shape a full name takes: a package's function, a method on a
// pointer, a method of an interface, a function without a package, and a variable, which is its name alone.
func holdsObjects() []types.Object {
	fmtPackage := types.NewPackage("fmt", "fmt")
	logPackage := types.NewPackage("log", "log")
	loggingPackage := types.NewPackage("example.com/logging", "logging")
	variadic := types.NewSignatureType(nil, nil, nil, types.NewTuple(
		types.NewParam(token.NoPos, nil, "format", types.Typ[types.String]),
		types.NewParam(token.NoPos, nil, "arguments", types.NewSlice(types.NewInterfaceType(nil, nil))),
	), nil, true)
	logger := types.NewNamed(types.NewTypeName(token.NoPos, logPackage, "Logger", nil), types.NewStruct(nil, nil), nil)
	pointerReceiver := types.NewSignatureType(
		types.NewParam(token.NoPos, logPackage, "logger", types.NewPointer(logger)), nil, nil, variadic.Params(), nil,
		true)
	sink := types.NewInterfaceType(nil, nil)
	interfaceReceiver := types.NewSignatureType(types.NewParam(token.NoPos, loggingPackage, "", sink), nil, nil,
		variadic.Params(), nil, true)
	return []types.Object{
		types.NewFunc(token.NoPos, fmtPackage, "Printf", variadic),
		types.NewFunc(token.NoPos, fmtPackage, "Errorf", variadic),
		types.NewFunc(token.NoPos, loggingPackage, "Record", variadic),
		types.NewFunc(token.NoPos, loggingPackage, "Warnf", variadic),
		types.NewFunc(token.NoPos, logPackage, "Printf", pointerReceiver),
		types.NewFunc(token.NoPos, loggingPackage, "Printf", interfaceReceiver),
		types.NewFunc(token.NoPos, nil, "printf", variadic),
		types.NewVar(token.NoPos, loggingPackage, "warn", variadic),
		types.NewVar(token.NoPos, loggingPackage, "Printf", variadic),
	}
}

// knownNames answers what the two lookups it stands in front of answer, the full name and the name lowercased, for
// every shape of full name, with and without names the -funcs flag gives without a dot.
func TestKnownNamesHoldWhatTheFullLookupsHold(test *testing.T) {
	test.Parallel()

	sets := map[string]stringSet{
		"the known functions": {"fmt.Printf": true, "fmt.Errorf": true, "(*log.Logger).Printf": true},
		"names without a dot": {"fmt.Printf": true, "warn": true, "warnf": true, "printf": true},
		"a method of an interface and a function without a package": {"(interface).Printf": true, "Printf": true},
	}
	for setName, functions := range sets {
		names := newKnownNames(functions)
		for _, object := range holdsObjects() {
			_, wanted := functions[fullName(object)]
			if !wanted {
				_, wanted = functions[strings.ToLower(object.Name())]
			}
			if held := names.holds(object); held != wanted {
				test.Errorf("%s: holds(%s) is %t, the full lookups %t", setName, fullName(object), held, wanted)
			}
		}
	}
}

// A function whose name is no known name's last segment is passed by without its full name or its name lowercased,
// each of which allocates, so a pass's many such callees cost nothing beyond a map lookup.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// move.
func TestKnownNamesPassAnUnknownNameByWithoutAllocating(test *testing.T) {
	names := newKnownNames(stringSet{"fmt.Printf": true, "(*log.Logger).Printf": true})
	record := types.NewFunc(token.NoPos, types.NewPackage("example.com/logging", "logging"), "Record",
		types.NewSignatureType(nil, nil, nil, nil, nil, false))
	allocations := testing.AllocsPerRun(100, func() {
		if names.holds(record) {
			test.Fatal("example.com/logging.Record is no known name")
		}
	})
	if allocations != 0 {
		test.Errorf("holds allocated %v times a call for a name no known name ends in, want 0", allocations)
	}
}
