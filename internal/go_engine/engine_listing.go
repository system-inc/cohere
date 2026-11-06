package go_engine

import (
	"fmt"
	"io"
	"runtime"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/settings"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// engineName is the provenance record's `engine`.
const engineName = "cohere-go"

// provenanceRecord says which engine wrote the stream. The engine is this cohere binary run as a child, so its
// version, commit and tree are cohere's own; the toolchain is the one that built it, which is the go/types and
// gofmt every finding came from.
func provenanceRecord() contract.ProvenanceRecord {
	current := release.Current()
	commit := current.SelfCommit
	if commit == "" {
		commit = "dev"
	}
	return contract.ProvenanceRecord{
		Contract:           contract.Version,
		Engine:             engineName,
		Version:            current.Version,
		Commit:             commit,
		SourceTreeModified: current.SourceTreeModified,
		Toolchain:          runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// writeVersion is `--version`: the provenance record alone.
func writeVersion(standardOutput io.Writer, standardError io.Writer) int {
	if writeError := contract.NewWriter(standardOutput).Write(provenanceRecord()); writeError != nil {
		return broken(standardError, writeError)
	}
	return exitClean
}

// writeRules is `--rules` and `--rules-enabled`: provenance, then one record per rule sorted by name, each at
// the severity the settings give it. `--rules-enabled` leaves out the rules the settings turn off. The front
// door sorts and prints them; the stream has no summary, by the contract's listing modes.
func writeRules(request invocation, registered []rule.Rule, standardOutput io.Writer, standardError io.Writer) int {
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		return refuse(standardError, settingsError)
	}
	names := knownRuleNames(registered)
	slices.Sort(names)

	writer := contract.NewWriter(standardOutput)
	if writeError := writer.Write(provenanceRecord()); writeError != nil {
		return broken(standardError, writeError)
	}
	for _, name := range names {
		severity := ruleSettings.Rules[name].Severity
		if request.mode() == modeRulesEnabled && severity == settings.SeverityOff {
			continue
		}
		if writeError := writer.Write(contract.RuleRecord{Name: name, Severity: string(severity)}); writeError != nil {
			return broken(standardError, writeError)
		}
	}
	return exitClean
}

// loadSettings reads which rules run, and how strictly, from the file `--lint-config` names or the one beside
// the root.
func loadSettings(request invocation, registered []rule.Rule) (settings.Settings, error) {
	return settings.Load(request.root, request.lintConfiguration, knownRuleNames(registered))
}

// broken says the stream could not be written, and returns the exit the front door reads as a crash.
func broken(standardError io.Writer, writeError error) int {
	// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
	_, _ = fmt.Fprintf(standardError, "cohere-go: the record stream could not be written, so it stops here: %v\n",
		writeError)
	return exitBroken
}
