package go_engine

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// contractArguments open every command line the front door sends.
var contractArguments = []string{"--contract", fmt.Sprint(contract.Version), "--root", "/project"}

func TestParseInvocationRefuses(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name      string
		arguments []string
		refusal   string
	}{
		{"no contract", []string{"--root", "/project"}, "no --contract was given"},
		{"a contract that is no number", []string{"--contract", "three", "--root", "/project"}, "not a version number"},
		{
			"a mismatch before an unknown flag", []string{"--contract", "4", "--frobnicate", "--root", "/project"},
			"the front door speaks contract 4 and this engine speaks contract 3",
		},
		{"an unknown flag", append(slices.Clone(contractArguments), "--frobnicate"), "--frobnicate is not a flag"},
		{"a flag with one dash", append(slices.Clone(contractArguments), "-fix"), "spelled with two dashes"},
		{"a switch neither true nor false", append(slices.Clone(contractArguments), "--fix=maybe"), "true or false"},
		{"a valued flag with no value", []string{"--contract", "3", "--root"}, "--root needs a value"},
		{"no root", []string{"--contract", "3"}, "no --root was given"},
		{"a relative root", []string{"--contract", "3", "--root", "project"}, "is not an absolute path"},
		{
			"a relative lint config", append(slices.Clone(contractArguments), "--lint-config", "Settings.json"),
			"--lint-config Settings.json is not an absolute path",
		},
		{"a relative path", append(slices.Clone(contractArguments), "inside"), "inside is not an absolute path"},
		{"unused", append(slices.Clone(contractArguments), "--unused-deep"), "--unused-deep is not implemented"},
		{"timing", append(slices.Clone(contractArguments), "--timing"), "--timing is not implemented"},
		{"explain", append(slices.Clone(contractArguments), "--explain=/project/a.go"), "--explain is not implemented"},
		{
			"format-only with no-format", append(slices.Clone(contractArguments), "--format-only", "--no-format"),
			"--format-only cannot be named with --no-format",
		},
		{
			"no-format with format-all", append(slices.Clone(contractArguments), "--no-format", "--format-all"),
			"--no-format cannot be named with --format-all",
		},
	}
	for _, refused := range cases {
		test.Run(refused.name, func(test *testing.T) {
			test.Parallel()
			_, parseError := parseInvocation(refused.arguments)
			if parseError == nil || !strings.Contains(parseError.Error(), refused.refusal) {
				test.Fatalf("refusal %v, want one saying %q", parseError, refused.refusal)
			}
		})
	}
}

func TestParseInvocationReads(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name      string
		arguments []string
		mode      mode
		requested []contract.PhaseName
		paths     []string
	}{
		{"no phase switch", contractArguments, modeCheck,
			[]contract.PhaseName{contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint}, nil},
		{"lint alone", append(slices.Clone(contractArguments), "--lint"), modeCheck,
			[]contract.PhaseName{contract.PhaseLint}, nil},
		{"fix and types", append(slices.Clone(contractArguments), "--fix", "--types=true"), modeCheck,
			[]contract.PhaseName{contract.PhaseFix, contract.PhaseTypes}, nil},
		{"a switch turned off asks for nothing", append(slices.Clone(contractArguments), "--lint=false"), modeCheck,
			[]contract.PhaseName{contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint}, nil},
		{"format-only", append(slices.Clone(contractArguments), "--format-only", "--no-fix"), modeCheck,
			[]contract.PhaseName{contract.PhaseFix}, nil},
		{"rules before version", append(slices.Clone(contractArguments), "--version", "--rules"), modeRules, nil, nil},
		{"rules-enabled", append(slices.Clone(contractArguments), "--rules-enabled"), modeRulesEnabled, nil, nil},
		{"version", append(slices.Clone(contractArguments), "--version"), modeVersion, nil, nil},
		// The front door forwards it for a cache the engine will keep of its own, so it is read, never refused.
		{"no-cache", append(slices.Clone(contractArguments), "--no-cache", "--no-fix"), modeCheck,
			[]contract.PhaseName{contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint}, nil},
		{"values after an equals sign", []string{"--contract=3", "--root=/project", "/project/a"}, modeCheck,
			[]contract.PhaseName{contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint}, []string{"/project/a"}},
		{"a path after -- that looks like a flag", append(slices.Clone(contractArguments), "--", "/project/--fix"),
			modeCheck, []contract.PhaseName{contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint},
			[]string{"/project/--fix"}},
	}
	for _, read := range cases {
		test.Run(read.name, func(test *testing.T) {
			test.Parallel()
			request, parseError := parseInvocation(read.arguments)
			if parseError != nil {
				test.Fatalf("refused: %v", parseError)
			}
			if request.mode() != read.mode {
				test.Errorf("mode %s, want %s", request.mode(), read.mode)
			}
			if read.mode == modeCheck {
				var requested []contract.PhaseName
				for _, phase := range []contract.PhaseName{
					contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint, contract.PhaseUnused,
				} {
					if request.requests(phase) {
						requested = append(requested, phase)
					}
				}
				if !slices.Equal(requested, read.requested) {
					test.Errorf("requested %v, want %v", requested, read.requested)
				}
			}
			if !slices.Equal(request.paths, read.paths) {
				test.Errorf("paths %q, want %q", request.paths, read.paths)
			}
		})
	}
}

func TestRefusalSentence(test *testing.T) {
	test.Parallel()
	cases := []struct {
		refusal error
		want    string
	}{
		{fmt.Errorf("reading %s: %w", "/project/go.work", errors.New("permission denied")),
			"Reading /project/go.work: permission denied."},
		// A trailing period here stands for an error from code we don't own, and it is the input under test:
		// refusalSentence must not add a second period to a foreign error's.
		//cohere:ignore consistency-require-error-clause the period is this case's input, as a foreign error carries one
		{errors.New("reading the go block. The go block takes \"rules\"."),
			"Reading the go block. The go block takes \"rules\"."},
		{errors.New("  "), "The Go engine refused the run and gave no reason."},
	}
	for _, sentence := range cases {
		if got := refusalSentence(sentence.refusal); got != sentence.want {
			test.Errorf("refusalSentence(%q) is %q, want %q", sentence.refusal, got, sentence.want)
		}
	}
}

// TestMainRefusesASecondRun holds the engine to one run per process, since a run's rule options stay set on the
// registry's analyzers and a second run would be judged by them.
//
// Not parallel: it sets `GOMEMLIMIT`, so Main leaves this test process's memory limit alone, and it spends the one
// Main this process may call.
func TestMainRefusesASecondRun(test *testing.T) {
	test.Setenv("GOMEMLIMIT", "off")
	versionArguments := append(slices.Clone(contractArguments), "--version")
	var firstOutput bytes.Buffer
	var firstError bytes.Buffer
	if exitCode := Main(versionArguments, &firstOutput, &firstError); exitCode != exitClean {
		test.Fatalf("the first run exited %d, want %d; stderr: %s", exitCode, exitClean, firstError.String())
	}
	var secondOutput bytes.Buffer
	var secondError bytes.Buffer
	if exitCode := Main(versionArguments, &secondOutput, &secondError); exitCode != exitRefused {
		test.Fatalf("the second run exited %d, want %d", exitCode, exitRefused)
	}
	if secondOutput.Len() != 0 || !strings.Contains(secondError.String(), "second engine run in one process") {
		test.Errorf("the second run wrote %q to stdout and %q to stderr, want nothing and the refusal",
			secondOutput.String(), secondError.String())
	}
}
