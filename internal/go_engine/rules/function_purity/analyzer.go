// Package function_purity judges which functions are pure, staticcheck's purity facts ported onto the engine's own
// intermediate representation, so the readers asking which calls have no side effects read them without buildir
// (#w7zmvat).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/facts/purity/purity.go, whose algorithm it
// keeps step for step: the package's source functions judged in the order buildir lists them, a callee of the same
// package judged as it is met, a callee still being judged answering not pure, so a cycle of calls is never pure while
// a function calling only itself can be, and each function judged pure marked with an IsPure fact on its object, which
// the packages importing it read. Its Result is every function the facts hold pure, this package's and its imports',
// keyed as staticcheck's is. The fact differential beside this file holds the two to the same facts over whole trees.
package function_purity

import (
	"fmt"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// Result is every function the facts hold pure, as staticcheck's purity.Result holds them.
type Result map[*types.Func]*IsPure

// Analyzer judges each function of the package, exporting IsPure on those that are pure, and returns every function
// the facts hold pure, the imported packages' among them.
var Analyzer = &analysis.Analyzer{
	Name:       "function_purity",
	Doc:        "marks the functions that are pure, staticcheck's purity facts on the engine's own representation",
	Run:        run,
	Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
	FactTypes:  []analysis.Fact{new(IsPure)},
	ResultType: reflect.TypeFor[Result](),
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

// run judges every source function of the package, in buildir's order, and returns every function the facts hold
// pure.
func run(pass *analysis.Pass) (any, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	judging := &judgment{pass: pass, irPackage: built.Package(), seen: map[*intermediate_representation.Function]bool{}}
	for function := range built.SourceFunctions() {
		judging.isPure(function)
	}
	result := Result{}
	for _, fact := range pass.AllObjectFacts() {
		function, isFunction := fact.Object.(*types.Func)
		pure, isPure := fact.Fact.(*IsPure)
		if !isFunction || !isPure {
			return nil, fmt.Errorf("reading function_purity's facts: %s holds a %T, where only a function's IsPure "+
				"was expected", fact.Object, fact.Fact)
		}
		result[function] = pure
	}
	return result, nil
}
