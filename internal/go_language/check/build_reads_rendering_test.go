package check

import (
	"bytes"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
)

// constantsPath is the path of the package constantsSource declares.
const constantsPath = "example.com/constants"

// constantsSource declares a constant of every kind: math's Pi and E, whose decimal digits no binary float holds
// exactly, an integer past int64, a string, a boolean and a complex number.
const constantsSource = "package constants\n\n" +
	"const Pi = 3.14159265358979323846264338327950288419716939937510582097494459\n\n" +
	"const E = 2.71828182845904523536028747135266249775724709369995957496696763\n\n" +
	"const Half float64 = 0.5\n\n" +
	"const Large = 1 << 80\n\n" +
	"const Small = 3\n\n" +
	"const Greeting = \"hello\"\n\n" +
	"const Enabled = true\n\n" +
	"const Rotation = 1.5 + 2i\n"

// A constant checked from source and the same constant read from export data render alike in a closure digest, though
// the value is held another way in each: math.Pi from source is a fraction of its decimal digits, and from export data
// a binary float. Two constants of different values still render apart.
func TestAConstantRendersAlikeFromSourceAndFromExportData(test *testing.T) {
	test.Parallel()

	source := checkedInMemory(test, constantsPath, constantsSource, nil)
	var exportData bytes.Buffer
	if writeError := gcexportdata.Write(&exportData, nil, source); writeError != nil {
		test.Fatal(writeError)
	}
	exported, readError := gcexportdata.Read(&exportData, token.NewFileSet(), map[string]*types.Package{},
		constantsPath)
	if readError != nil {
		test.Fatal(readError)
	}
	rendered := map[string]string{}
	for _, name := range source.Scope().Names() {
		fromSource := ownRendering(source.Scope().Lookup(name))
		fromExportData := ownRendering(exported.Scope().Lookup(name))
		if fromSource != fromExportData {
			test.Errorf("%s renders apart:\nfrom source      %s\nfrom export data %s", name, fromSource,
				fromExportData)
		}
		rendered[name] = fromSource
	}
	sourcePi, sourceIsConstant := source.Scope().Lookup("Pi").(*types.Const)
	exportedPi, exportedIsConstant := exported.Scope().Lookup("Pi").(*types.Const)
	if !sourceIsConstant || !exportedIsConstant {
		test.Fatal("Pi is not a constant on both sides")
	}
	if sourcePi.Val().ExactString() == exportedPi.Val().ExactString() {
		test.Error("Pi is held alike from source and from export data, so the fixture tells nothing")
	}
	for _, pair := range [][2]string{{"Pi", "E"}, {"Large", "Small"}} {
		left := rendered[pair[0]][len("const of "+constantsPath+" "+pair[0]):]
		right := rendered[pair[1]][len("const of "+constantsPath+" "+pair[1]):]
		if left == right {
			test.Errorf("%s and %s render one value, %s", pair[0], pair[1], left)
		}
	}
}
