package go_engine

// The engine side of cohere-go/consistency-no-utils-folder's module root (#mxe7xjy). The rule judges a package's path
// below its module root, which it reads from pass.Module, and the pass has a module only because load.Packages asks
// go list for it. analysistest's own loader asks for the module whatever the engine does, so only a run through the
// engine's load can show the module reaches the rule.

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/conformance/text_rules"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_utils_folder"
	"github.com/system-inc/cohere/internal/go_engine/settings"
)

// utilsModuleFindings runs the utils-folder rule over the module in directory as runTextRulesCase runs a case, and
// returns the file of each of its findings.
func utilsModuleFindings(test *testing.T, directory string) []string {
	test.Helper()
	rulesForModule := []rule.Rule{consistency_no_utils_folder.Rule}
	ruleSettings, settingsError := settings.Load(directory, "", knownRuleNames(rulesForModule))
	if settingsError != nil {
		test.Fatalf("the module's settings are refused: %v", settingsError)
	}
	if optionsError := configureRules(ruleSettings, rulesForModule); optionsError != nil {
		test.Fatalf("the module's rule options are refused: %v", optionsError)
	}
	loaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading the module: %v", loadError)
	}
	for _, diagnostic := range loaded.Diagnostics {
		test.Errorf("the module does not compile, so it measures nothing: %s:%d:%d: %s", diagnostic.File,
			diagnostic.Line, diagnostic.Column, diagnostic.Message)
	}
	if len(loaded.Diagnostics) > 0 {
		test.FailNow()
	}
	checked, runError := check.Run(loaded.Packages, rulesForModule, check.Options{})
	if runError != nil {
		test.Fatalf("running %s: %v", consistency_no_utils_folder.Name, runError)
	}
	for _, crash := range checked.Crashes {
		test.Errorf("%s crashed: %s", consistency_no_utils_folder.Name, crash.Error)
	}
	reported := []string{}
	for _, finding := range checked.Findings {
		if finding.Rule == consistency_no_utils_folder.Name {
			reported = append(reported, finding.File)
		}
	}
	return reported
}

// A module whose own path ends in utils is nobody's to rename from inside it, so its root package and its helpers
// package are clean. A Util folder below helpers is inside the module, and is its one finding, which also shows the
// clean half came from a rule that ran.
func TestUtilsFolderJudgesBelowTheModuleRoot(test *testing.T) {
	test.Parallel()
	// Resolved, so the file names the load reports are the ones written here.
	directory, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeTextRulesFile(test, directory, "go.mod", fmt.Appendf(nil, "module example.com/utils\n\ngo %s\n",
		textRulesGoVersion(test)))
	writeTextRulesFile(test, directory, textRulesSettingsFileName,
		textRulesSettings(test, []string{consistency_no_utils_folder.Name}, text_rules.Case{}))
	writeTextRulesFile(test, directory, "root.go", []byte("package root\n\nconst Root = 1\n"))
	writeTextRulesFile(test, directory, "helpers/helpers.go", []byte("package helpers\n\nconst Helper = 1\n"))

	if reported := utilsModuleFindings(test, directory); len(reported) > 0 {
		test.Errorf("the module named example.com/utils was reported at %v, though nothing below its root is a "+
			"banned folder", reported)
	}

	deep := writeTextRulesFile(test, directory, "helpers/Util/deep.go", []byte("package deep\n\nconst Deep = 1\n"))
	if reported := utilsModuleFindings(test, directory); !slices.Equal(reported, []string{deep}) {
		test.Errorf("the rule reported %v, and only %s is in a banned folder below the module root", reported, deep)
	}
}
