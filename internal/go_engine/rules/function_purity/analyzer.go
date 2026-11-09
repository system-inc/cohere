// Package function_purity judges which functions are pure, staticcheck's purity facts ported onto the engine's own
// intermediate representation, so the readers asking which calls have no side effects read them without buildir
// (#w7zmvat).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/facts/purity/purity.go, whose judgment of a
// function's body it keeps step for step: listed pure, or returning a result from parameters of basic types only,
// through a body that calls only pure functions and touches only its own stack. The original pushes its facts: it
// judges every source function of every package, dependencies included, in the order buildir lists them, a callee of
// the same package as it is met, a callee still being judged answering not pure, and marks each function judged pure
// with a fact its importers read. Here a reader asks instead (#w7zmvat unit 2): Result.IsPure judges a function only
// when asked, through the run's fact resolver, building only what that judgment reaches, and gives every answer the
// push gives. demand_walk.go says how; the push walk stays in push_walk.go, for the resolver's fallback and the tests.
package function_purity

import (
	"fmt"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// Analyzer hands the package's readers its purity, which they ask of one function at a time. It requires ctrlflow, as
// the build of every package it judges does, so ctrlflow has run on the package and on every package below it, and
// the resolver holds each of them, before a reader asks.
var Analyzer = &analysis.Analyzer{
	Name:       "function_purity",
	Doc:        "judges which functions are pure when a reader asks, staticcheck's purity on the engine's own representation",
	Run:        run,
	Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

// pureStandardLibrary is the standard library functions held pure whatever their bodies do. It is a copy of
// staticcheck's pureStdlib, honnef.co/go/tools@v0.8.1 analysis/facts/purity/purity.go, kept entry for entry, since a
// function on one list and not the other would be pure to one side alone.
var pureStandardLibrary = map[string]struct{}{
	"errors.New":                      {},
	"fmt.Errorf":                      {},
	"fmt.Sprintf":                     {},
	"fmt.Sprint":                      {},
	"sort.Reverse":                    {},
	"strings.Map":                     {},
	"strings.Repeat":                  {},
	"strings.Replace":                 {},
	"strings.Title":                   {},
	"strings.ToLower":                 {},
	"strings.ToLowerSpecial":          {},
	"strings.ToTitle":                 {},
	"strings.ToTitleSpecial":          {},
	"strings.ToUpper":                 {},
	"strings.ToUpperSpecial":          {},
	"strings.Trim":                    {},
	"strings.TrimFunc":                {},
	"strings.TrimLeft":                {},
	"strings.TrimLeftFunc":            {},
	"strings.TrimPrefix":              {},
	"strings.TrimRight":               {},
	"strings.TrimRightFunc":           {},
	"strings.TrimSpace":               {},
	"strings.TrimSuffix":              {},
	"(*net/http.Request).WithContext": {},
	"time.Now":                        {},
	"time.Parse":                      {},
	"time.ParseInLocation":            {},
	"time.Unix":                       {},
	"time.UnixMicro":                  {},
	"time.UnixMilli":                  {},
	"(time.Time).Add":                 {},
	"(time.Time).AddDate":             {},
	"(time.Time).After":               {},
	"(time.Time).Before":              {},
	"(time.Time).Clock":               {},
	"(time.Time).Compare":             {},
	"(time.Time).Date":                {},
	"(time.Time).Day":                 {},
	"(time.Time).Equal":               {},
	"(time.Time).Format":              {},
	"(time.Time).GoString":            {},
	"(time.Time).GobEncode":           {},
	"(time.Time).Hour":                {},
	"(time.Time).ISOWeek":             {},
	"(time.Time).In":                  {},
	"(time.Time).IsDST":               {},
	"(time.Time).IsZero":              {},
	"(time.Time).Local":               {},
	"(time.Time).Location":            {},
	"(time.Time).MarshalBinary":       {},
	"(time.Time).MarshalJSON":         {},
	"(time.Time).MarshalText":         {},
	"(time.Time).Minute":              {},
	"(time.Time).Month":               {},
	"(time.Time).Nanosecond":          {},
	"(time.Time).Round":               {},
	"(time.Time).Second":              {},
	"(time.Time).String":              {},
	"(time.Time).Sub":                 {},
	"(time.Time).Truncate":            {},
	"(time.Time).UTC":                 {},
	"(time.Time).Unix":                {},
	"(time.Time).UnixMicro":           {},
	"(time.Time).UnixMilli":           {},
	"(time.Time).UnixNano":            {},
	"(time.Time).Weekday":             {},
	"(time.Time).Year":                {},
	"(time.Time).YearDay":             {},
	"(time.Time).Zone":                {},
	"(time.Time).ZoneBounds":          {},
}

// run is the package's purity, asked of the run's resolver as the package's readers ask.
func run(pass *analysis.Pass) (any, error) {
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	reader := resolver.Package(pass.Pkg)
	if reader == nil {
		return nil, fmt.Errorf("reading the purity of %s: the fact resolver never learned it, though ctrlflow, "+
			"which it learns a package from, ran on it first", pass.Pkg.Path())
	}
	return &Result{resolver: resolver, reader: reader}, nil
}
