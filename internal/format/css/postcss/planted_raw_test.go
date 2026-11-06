package postcss

import (
	"strings"
	"testing"
)

// plantedRaw is a raw kind toEstree has no arm for, standing in for one a later port adds to rawEntry.
type plantedRaw struct{}

func (plantedRaw) rawEntry() {}

// TestToEstreeRefusesARawKindItHasNoArmFor holds toEstree's default: a raw of a kind its switch does not name
// panics with the key and the kind, which Parse turns into an error, rather than leaving the raw out of the node.
func TestToEstreeRefusesARawKindItHasNoArmFor(test *testing.T) {
	test.Parallel()
	root := newRoot()
	root.raws["planted"] = plantedRaw{}
	defer func() {
		recovered := recover()
		message, isString := recovered.(string)
		if !isString || !strings.Contains(message, "raws.planted") || !strings.Contains(message, "plantedRaw") {
			test.Errorf(
				"toEstree with a planted raw kind recovered %#v, want a panic naming raws.planted and plantedRaw",
				recovered,
			)
		}
	}()
	toEstree(root, newInput(""))
	test.Error("toEstree returned with a raw kind it has no arm for")
}
