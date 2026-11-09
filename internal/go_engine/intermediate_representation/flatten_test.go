package intermediate_representation_test

import (
	"fmt"
	"strings"
	"testing"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// flattenSource holds a map update for each answer Flatten gives: a map made where it's written, a nil map carried
// round a loop's phi, which is that one constant, and a phi of a made map and nil, which is no one value.
const flattenSource = `package flatten

func made() {
	written := map[string]int{}
	written["a"] = 1
}

func looped(count int) {
	var written map[string]int
	for index := 0; index < count; index++ {
		written["a"] = index
	}
}

func joined(choose bool) {
	written := map[string]int{}
	if choose {
		written = nil
	}
	written["a"] = 1
}
`

// Flatten reads the same value through our phis as irutil.Flatten reads through go/ir's, built lifted from the same
// source: for each map update, the map's one value, named by its kind or, for a constant, by its value and type.
func TestFlattenReadsWhatIrutilReads(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "flatten", flattenSource)
	theirPackage := differentialBuildTheirs(unit, 0)
	ourPackage := differentialBuildOurs(unit, false)
	expected := map[string]string{"made": "MakeMap", "looped": "nil:map[string]int", "joined": "no one value"}
	for name, flattened := range expected {
		theirs := []string{}
		for _, block := range theirPackage.Func(name).Blocks {
			for _, instruction := range block.Instrs {
				if update, isUpdate := instruction.(*ir.MapUpdate); isUpdate {
					theirs = append(theirs, flattenDescribeTheirs(irutil.Flatten(update.Map)))
				}
			}
		}
		ours := []string{}
		for _, block := range ourPackage.Function(name).Blocks {
			for _, instruction := range block.Instructions {
				if update, isUpdate := instruction.(*intermediate_representation.MapUpdate); isUpdate {
					ours = append(ours, flattenDescribeOurs(intermediate_representation.Flatten(update.Map)))
				}
			}
		}
		if len(theirs) != 1 || len(ours) != 1 || theirs[0] != flattened || ours[0] != flattened {
			test.Errorf("in %s irutil.Flatten read %q and ours %q, where %q was expected of each", name, theirs, ours,
				flattened)
		}
	}
}

// flattenDescribeTheirs names what irutil.Flatten returned: no one value for nil, a constant's value and type for a
// constant, and otherwise its kind, the type's name without its package.
func flattenDescribeTheirs(value ir.Value) string {
	if value == nil {
		return "no one value"
	}
	if constant, isConstant := value.(*ir.Const); isConstant {
		return constant.String()
	}
	return flattenKind(fmt.Sprintf("%T", value))
}

// flattenDescribeOurs names what our Flatten returned, as flattenDescribeTheirs names go/ir's.
func flattenDescribeOurs(value intermediate_representation.Value) string {
	if value == nil {
		return "no one value"
	}
	if constant, isConstant := value.(*intermediate_representation.Constant); isConstant {
		return constant.String()
	}
	return flattenKind(fmt.Sprintf("%T", value))
}

// flattenKind is typeName, a value's type as %T prints it, without its package, the same for a kind both
// representations name alike.
func flattenKind(typeName string) string {
	return typeName[strings.LastIndex(typeName, ".")+1:]
}
