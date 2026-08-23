// Running both gates for real, and proving the harness saw a difference before it is believed.
//
// Everything else in this package is pure: findings in, report out, easy to test and easy to trust.
// This file is where the harness touches the world, and it is therefore where the interesting lies
// live. A subprocess that fails to launch, a working directory that is not the tree, a config that
// was never read, a regex that stopped matching after an output format moved — every one of them
// produces a smaller finding list, and a smaller finding list on both sides is indistinguishable
// from agreement.
//
// So this file does not report a comparison. It reports a comparison plus the evidence that the
// comparison happened, and it plants a known violation to prove the pipeline can carry one end to
// end before any empty result is allowed to mean anything.
package differential

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// GateCommand is how to invoke one side.
type GateCommand struct {
	// Name is what appears in the report.
	Name string
	// Program and Arguments are the exact invocation.
	Program   string
	Arguments []string
	// Directory is the working directory, which for both gates is the tree being linted. Getting
	// this wrong is the single most likely way to produce a confident empty result: both gates
	// resolve their config and their file list from here.
	Directory string
}

// String renders the invocation so a reader can rerun it verbatim.
func (command GateCommand) String() string {
	return strings.TrimSpace(command.Program + " " + strings.Join(command.Arguments, " "))
}

// RunOptions is everything Run needs to compare the two gates over one tree.
type RunOptions struct {
	// Root is the tree both gates lint, and the directory paths are made relative to.
	Root string
	// Verify and Gate are the two invocations.
	Verify GateCommand
	Gate   GateCommand
	// VerifyRules is every rule name compiled into verify, and ConfiguredRules every rule the lint
	// config enables. Both are read by the caller rather than inferred here, for the reason stated
	// on Inputs: a rule that works and finds nothing is invisible in the output.
	VerifyRules     map[string]bool
	ConfiguredRules map[string]bool
	// Controls are the planted violations that prove the pipeline can carry a difference. Running
	// with none is allowed and produces a report that says, in as many words, that it proved
	// nothing — rather than a clean one.
	Controls []Control
}

// Control is a violation planted where exactly one gate can see it.
//
// The file is written into the tree, both gates run, and the harness checks that the difference
// came out the far end attributed to the expected side. That is an end-to-end proof: the process
// launched, the output parsed, the paths normalized, the rule names collapsed, and the comparison
// reported. Any one of those failing silently makes the control miss.
type Control struct {
	// Name identifies the control in the report.
	Name string
	// RelativePath is where the file is written, relative to Root. It must be a path both gates
	// actually lint: a control placed somewhere the config ignores misses for a reason that has
	// nothing to do with the harness, and looks identical to a broken pipeline.
	RelativePath string
	// Contents is the file to write.
	Contents string
	// Rule is the rule expected to fire.
	Rule string
	// ExpectedSide is the gate that should see it alone.
	ExpectedSide Side
}

// Run executes both gates over the tree, plants any controls, and returns the compared report.
//
// The order is deliberate: controls are planted first so that both gates see them in the same
// single run. Running the gates twice, once clean and once planted, would double the cost and
// introduce a window in which the tree changed underneath the two runs — and on a tree with several
// authors in it, that window is not theoretical.
func Run(ctx context.Context, options RunOptions) (Report, error) {
	planted, cleanup, err := plantControls(options)
	// cleanup is deferred before the error check on purpose: plantControls may have written some
	// files before failing, and leaving a planted violation behind in a shared tree would be a
	// defect handed to whoever runs the gate next.
	defer cleanup()
	if err != nil {
		return Report{}, err
	}

	verifyOutput, verifyErr := runGate(ctx, options.Verify)
	if verifyErr != nil {
		return Report{}, fmt.Errorf("running verify: %w", verifyErr)
	}
	gateOutput, gateErr := runGate(ctx, options.Gate)
	if gateErr != nil {
		return Report{}, fmt.Errorf("running the gate: %w", gateErr)
	}

	verifyParsed, err := ParseVerify(verifyOutput, options.Root)
	if err != nil {
		return Report{}, fmt.Errorf("parsing verify output: %w", err)
	}
	gateParsed, err := ParseGate(gateOutput, options.Root)
	if err != nil {
		return Report{}, fmt.Errorf("parsing gate output: %w", err)
	}

	// An unparsed line is a finding the diff cannot see, so it is a hard failure rather than a
	// note. Continuing past it would compare two partial lists and report the difference between
	// them as though it were the difference between the gates.
	if len(verifyParsed.UnparsedLines) > 0 {
		return Report{}, fmt.Errorf(
			"verify printed %d lines this harness could not parse, so its findings are incomplete; first: %q",
			len(verifyParsed.UnparsedLines), verifyParsed.UnparsedLines[0],
		)
	}
	if len(gateParsed.UnparsedLines) > 0 {
		return Report{}, fmt.Errorf(
			"the gate printed %d lines this harness could not parse, so its findings are incomplete; first: %q",
			len(gateParsed.UnparsedLines), gateParsed.UnparsedLines[0],
		)
	}

	verifyFilesWalked, verifyRulesRun := verifyCoverageFrom(verifyParsed.SummaryLines)
	// The gate prints no coverage line of its own, so its population is the file count verify
	// reported for the same tree. That is a borrowed number and it is marked as such here rather
	// than presented as the gate's own claim: it establishes that the tree was non-empty, which is
	// the property the vacuity guard needs, and nothing more.
	gateFilesWalked := verifyFilesWalked

	report := Compare(Inputs{
		VerifyFindings:   verifyParsed.Findings,
		GateFindings:     gateParsed.Findings,
		VerifyPopulation: Population{Findings: len(verifyParsed.Findings), FilesWalked: verifyFilesWalked, Rules: verifyRulesRun},
		GatePopulation:   Population{Findings: len(gateParsed.Findings), FilesWalked: gateFilesWalked},
		VerifyRules:      options.VerifyRules,
		ConfiguredRules:  options.ConfiguredRules,
	})

	report.Provenance = Provenance{
		VerifyFilesLinted: verifyFilesWalked,
		GateFilesLinted:   gateFilesWalked,
		VerifyRulesRun:    verifyRulesRun,
		GateRulesRun:      len(options.ConfiguredRules),
		VerifyCommand:     options.Verify.String(),
		GateCommand:       options.Gate.String(),
		ControlsRun:       checkControls(planted, report),
	}

	return report, nil
}

// checkControls asks, for each planted violation, whether the comparison actually reported it.
//
// This is the end-to-end assertion and the reason the whole file exists. It deliberately looks in
// report.Differences rather than in either gate's raw findings: a control that one gate found but
// the comparison dropped is exactly the bug worth catching, and checking the raw output instead
// would sail straight past it.
func checkControls(planted []Control, report Report) []ControlResult {
	results := make([]ControlResult, 0, len(planted))
	for _, control := range planted {
		result := ControlResult{
			Name:         control.Name,
			ExpectedSide: control.ExpectedSide,
			Rule:         control.Rule,
		}

		for _, difference := range report.Differences {
			if difference.Finding.Rule != control.Rule {
				continue
			}
			if !strings.HasSuffix(filepath.ToSlash(difference.Finding.File), filepath.ToSlash(control.RelativePath)) {
				continue
			}
			if difference.OnlyOn != control.ExpectedSide {
				result.Detail = fmt.Sprintf("found, but attributed to %s rather than %s", difference.OnlyOn, control.ExpectedSide)
				break
			}
			result.Detected = true
			result.Detail = ""
			break
		}

		if !result.Detected && result.Detail == "" {
			result.Detail = fmt.Sprintf("no difference reported for %s in %s", control.Rule, control.RelativePath)
		}
		results = append(results, result)
	}
	return results
}

// plantControls writes each control into the tree and returns a cleanup that removes them.
//
// It refuses to overwrite an existing file. On a tree with several authors in it, clobbering
// somebody's work to run a self-test would be a worse defect than the one the self-test is looking
// for, and a control that silently replaced a real file would also be linted for the wrong reasons.
func plantControls(options RunOptions) ([]Control, func(), error) {
	planted := make([]Control, 0, len(options.Controls))
	written := make([]string, 0, len(options.Controls))

	cleanup := func() {
		for _, path := range written {
			// A control left behind would be linted by the next person to run either gate, so a
			// failure to remove one is worth saying out loud rather than swallowing.
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "differential: could not remove planted control %s: %v\n", path, err)
			}
		}
	}

	for _, control := range options.Controls {
		absolutePath := filepath.Join(options.Root, control.RelativePath)
		if _, err := os.Stat(absolutePath); err == nil {
			return planted, cleanup, fmt.Errorf(
				"control %q would overwrite the existing file %s; choose a path that does not exist",
				control.Name, control.RelativePath,
			)
		}
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			return planted, cleanup, fmt.Errorf("creating the directory for control %q: %w", control.Name, err)
		}
		if err := os.WriteFile(absolutePath, []byte(control.Contents), 0o644); err != nil {
			return planted, cleanup, fmt.Errorf("writing control %q: %w", control.Name, err)
		}
		written = append(written, absolutePath)
		planted = append(planted, control)
	}

	return planted, cleanup, nil
}

// runGate executes one side and returns its stdout.
//
// A non-zero exit is expected and is not an error: both gates exit 1 when they find something, which
// is the common case. Only a failure to run at all is an error — and it must be one, because a gate
// that never launched produces empty output that reads exactly like a clean result.
func runGate(ctx context.Context, command GateCommand) (string, error) {
	execution := exec.CommandContext(ctx, command.Program, command.Arguments...)
	execution.Dir = command.Directory

	output, err := execution.Output()
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return "", fmt.Errorf("%s did not run: %w", command, err)
		}
		// It ran and exited non-zero, which is what a gate with findings does.
		return string(output), nil
	}
	return string(output), nil
}

// verifyCoverageLinePattern reads the population out of verify's own coverage line:
//
//	lint: 1 findings — 23 rules over 3407 files, 2098302 nodes visited, in 813ms
var verifyCoverageLinePattern = regexp.MustCompile(`^lint: \d+ findings? — (\d+) rules? over (\d+) files?`)

// verifyCoverageFrom reads files-walked and rules-run out of verify's summary lines.
//
// Returning zeros when the line is absent is deliberate and is not a fallback. A missing coverage
// line means verify did not finish its lint phase, and zero is precisely the value that makes the
// vacuity guard refuse the comparison — which is the correct outcome for a run that did not
// complete. Defaulting to a plausible number here would be the vacuous pass this package exists to
// prevent, introduced by the file that reports the evidence.
func verifyCoverageFrom(summaryLines []string) (int, int) {
	for _, line := range summaryLines {
		match := verifyCoverageLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		rulesRun, _ := strconv.Atoi(match[1])
		filesWalked, _ := strconv.Atoi(match[2])
		return filesWalked, rulesRun
	}
	return 0, 0
}
