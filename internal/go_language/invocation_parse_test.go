package go_language

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/contract"
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
			"a mismatch before an unknown flag", []string{"--contract", "5", "--frobnicate", "--root", "/project"},
			"the front door speaks contract 5 and cohere-go speaks contract 4",
		},
		{"an unknown flag", append(slices.Clone(contractArguments), "--frobnicate"), "--frobnicate is not a flag"},
		// Every build is checked unless `--host-build-only` is named (#qqyq61s), so the switch that asked for them is
		// gone, refused as any flag cohere-go does not take.
		{
			"the switch that asked for every build", append(slices.Clone(contractArguments), "--other-builds"),
			"--other-builds is not a flag cohere-go takes",
		},
		{"a flag with one dash", append(slices.Clone(contractArguments), "-fix"), "spelled with two dashes"},
		{"a switch neither true nor false", append(slices.Clone(contractArguments), "--fix=maybe"), "true or false"},
		{"a valued flag with no value", []string{"--contract", "4", "--root"}, "--root needs a value"},
		{"no root", []string{"--contract", "4"}, "no --root was given"},
		{"a relative root", []string{"--contract", "4", "--root", "project"}, "is not an absolute path"},
		{
			"a relative lint config", append(slices.Clone(contractArguments), "--lint-config", "Settings.json"),
			"--lint-config Settings.json is not an absolute path",
		},
		{"a relative path", append(slices.Clone(contractArguments), "inside"), "inside is not an absolute path"},
		{"unused", append(slices.Clone(contractArguments), "--unused-deep"), "--unused-deep is not implemented"},
		{"explain", append(slices.Clone(contractArguments), "--explain=/project/a.go"), "--explain is not implemented"},
		{
			"format-only with no-format", append(slices.Clone(contractArguments), "--format-only", "--no-format"),
			"--format-only cannot be named with --no-format",
		},
		{
			"no-format with format-all", append(slices.Clone(contractArguments), "--no-format", "--format-all"),
			"--no-format cannot be named with --format-all",
		},
		{
			"a relative save", append(slices.Clone(contractArguments), "--fix", "--stdin-filepath", "value.go"),
			"--stdin-filepath value.go is not an absolute path",
		},
		{
			"a save without fix", append(slices.Clone(contractArguments), "--stdin-filepath", "/project/value.go"),
			"--stdin-filepath answers what --fix writes for one file",
		},
		{
			"a save that lints", append(slices.Clone(contractArguments), "--fix", "--lint", "--stdin-filepath",
				"/project/value.go"),
			"--stdin-filepath answers what --fix writes for one file",
		},
		{
			"a save beside a path", append(slices.Clone(contractArguments), "--fix", "--stdin-filepath",
				"/project/value.go", "/project/other.go"),
			"--stdin-filepath answers what --fix writes for one file",
		},
		// Only a check run has phases and builds to map, so every other run refuses `--timeline` by name rather than
		// accept it and write no timeline (#3v2zp7g).
		{
			"a timeline beside the rules", append(slices.Clone(contractArguments), "--rules", "--timeline"),
			"--timeline maps a check run's phases and builds, and this run lists the rules, so it was refused",
		},
		{
			"a timeline beside the enabled rules", append(slices.Clone(contractArguments), "--rules-enabled",
				"--timeline"),
			"this run lists the rules the settings turn on, so it was refused",
		},
		{
			"a timeline beside the version", append(slices.Clone(contractArguments), "--version", "--timeline"),
			"this run prints the version, so it was refused",
		},
		{
			"a timeline beside a save", append(slices.Clone(contractArguments), "--fix", "--timeline",
				"--stdin-filepath", "/project/value.go"),
			"this run is the editor's save, which prints one file's text, so it was refused",
		},
		// A caller that asked for a profile would find none, so an empty file name is refused, never read as no profile
		// (#r33n96f).
		{
			"a profile naming no file", append(slices.Clone(contractArguments), "--profile="),
			"--profile names no file to write the profile to",
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
		{"a save", append(slices.Clone(contractArguments), "--fix", "--format", "--stdin-filepath",
			"/project/value.go"), modeStdin, nil, nil},
		// The front door forwards it for a cache cohere-go will keep of its own, so it is read, never refused.
		{"no-cache", append(slices.Clone(contractArguments), "--no-cache", "--no-fix"), modeCheck,
			[]contract.PhaseName{contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint}, nil},
		// The system cache's switch is read as the project store's is (#syscache).
		{"no-system-cache", append(slices.Clone(contractArguments), "--no-system-cache", "--no-fix"), modeCheck,
			[]contract.PhaseName{contract.PhaseFix, contract.PhaseTypes, contract.PhaseLint}, nil},
		{"values after an equals sign", []string{"--contract=4", "--root=/project", "/project/a"}, modeCheck,
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

// `--profile` names the file the process's CPU profile goes to, in either spelling, and nothing else (#r33n96f): the
// file is no path the run narrows to, the run asks the phases it would without it, and a run without one names none.
func TestTheProfileNamesAFileAndNoPath(test *testing.T) {
	test.Parallel()
	for name, arguments := range map[string][]string{
		"a separate value": append(slices.Clone(contractArguments), "--profile", "/tmp/run.go.pprof", "--no-fix"),
		"a joined value":   append(slices.Clone(contractArguments), "--profile=/tmp/run.go.pprof", "--no-fix"),
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			request, parseError := parseInvocation(arguments)
			if parseError != nil {
				test.Fatalf("refused: %v", parseError)
			}
			if request.profilePath != "/tmp/run.go.pprof" || len(request.paths) != 0 || request.mode() != modeCheck {
				test.Errorf("profile %q, paths %q, mode %s, want the file, no path and a check", request.profilePath,
					request.paths, request.mode())
			}
		})
	}
	request, parseError := parseInvocation(append(slices.Clone(contractArguments), "--no-fix"))
	if parseError != nil || request.profilePath != "" {
		test.Errorf("a run without --profile names %q (refusal %v), want none", request.profilePath, parseError)
	}
}

// A run checks every build the census needs beside the host's unless `--host-build-only` is named (#qqyq61s): no switch
// at all checks every build, the switch narrows the run to the host's, and the switch turned off asks for nothing.
func TestEveryBuildIsCheckedUnlessHostBuildOnlyIsNamed(test *testing.T) {
	test.Parallel()
	for _, read := range []struct {
		name       string
		arguments  []string
		everyBuild bool
	}{
		{"no switch", contractArguments, true},
		{"the switch", append(slices.Clone(contractArguments), "--host-build-only"), false},
		{"the switch turned off", append(slices.Clone(contractArguments), "--host-build-only=false"), true},
	} {
		request, parseError := parseInvocation(read.arguments)
		if parseError != nil {
			test.Fatalf("%s was refused: %v", read.name, parseError)
		}
		if request.checksEveryBuild() != read.everyBuild {
			test.Errorf("%s checks every build %t, want %t", read.name, request.checksEveryBuild(), read.everyBuild)
		}
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
		{errors.New("  "), "cohere-go refused the run and gave no reason."},
	}
	for _, sentence := range cases {
		if got := refusalSentence(sentence.refusal); got != sentence.want {
			test.Errorf("refusalSentence(%q) is %q, want %q", sentence.refusal, got, sentence.want)
		}
	}
}

// TestMainRefusesASecondRun holds cohere-go to one run per process, since a run's rule options stay set on the
// registry's analyzers and a second run would be judged by them.
//
// Not parallel: it sets `GOMEMLIMIT`, so Main leaves this test process's memory limit alone, and it spends the one
// Main this process may call.
func TestMainRefusesASecondRun(test *testing.T) {
	test.Setenv("GOMEMLIMIT", "off")
	versionArguments := append(slices.Clone(contractArguments), "--version")
	var firstOutput bytes.Buffer
	var firstError bytes.Buffer
	if exitCode := Main(versionArguments, 0, nil, &firstOutput, &firstError); exitCode != exitClean {
		test.Fatalf("the first run exited %d, want %d; stderr: %s", exitCode, exitClean, firstError.String())
	}
	var secondOutput bytes.Buffer
	var secondError bytes.Buffer
	if exitCode := Main(versionArguments, 0, nil, &secondOutput, &secondError); exitCode != exitRefused {
		test.Fatalf("the second run exited %d, want %d", exitCode, exitRefused)
	}
	if secondOutput.Len() != 0 || !strings.Contains(secondError.String(), "second run in one process") {
		test.Errorf("the second run wrote %q to stdout and %q to stderr, want nothing and the refusal",
			secondOutput.String(), secondError.String())
	}
}

// A run hashes cohere-go's executable early only when it may key something by cohere-go's identity
// (keysByIdentity): never under `--no-cache --no-system-cache`, which keys nothing, nor for a run that only formats,
// and always for a run that loads its packages with a project store or a system cache to key.
func TestOnlyARunThatMayKeyHashesTheExecutableEarly(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		arguments []string
		want      bool
	}{
		{arguments: nil, want: true},
		{arguments: []string{"--no-cache"}, want: true},
		{arguments: []string{"--no-system-cache"}, want: true},
		{arguments: []string{"--lint", "--no-cache"}, want: true},
		{arguments: []string{"--no-cache", "--no-system-cache"}, want: false},
		{arguments: []string{"--lint", "--no-cache", "--no-system-cache"}, want: false},
		{arguments: []string{"--format-only"}, want: false},
	} {
		request, parseError := parseInvocation(append([]string{"--contract", fmt.Sprint(contract.Version), "--root",
			test.TempDir()}, each.arguments...))
		if parseError != nil {
			test.Fatalf("%q: %v", each.arguments, parseError)
		}
		if got := request.keysByIdentity(); got != each.want {
			test.Errorf("%q: hashes cohere-go early %t, want %t", each.arguments, got, each.want)
		}
	}
}
