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

// Control is a violation planted where the harness knows in advance what should happen to it.
//
// The file is written into the tree, both gates run, and the harness checks that the expected
// outcome came out the far end. That is an end-to-end proof: the process launched, the output
// parsed, the paths normalized, the rule names collapsed across two decoration schemes, and the
// comparison ran. Any one of those failing silently makes the control miss.
//
// There are two shapes, and they prove different things.
//
// A one-sided control (ExpectedSide set) proves the harness can report a difference in a named
// direction. It is the stronger claim and it requires a real divergence to exist between the two
// gates, which cannot be manufactured on demand.
//
// A shared control (ExpectedShared) proves the pipeline carries a finding end to end and that both
// normalizations worked, without claiming anything about direction. It is the weaker claim, and it
// is honest about being weaker: ControlsProven ignores shared controls entirely, so a run carrying
// only these still reports that detection was never demonstrated.
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
	// ExpectedSide is the gate that should see it alone. Ignored when ExpectedShared is set.
	ExpectedSide Side
	// ExpectedShared says both gates must report this finding at the same place, so it appears as a
	// match rather than as a difference. Set for a rule both sides implement and the config enables,
	// where a one-sided result would be the defect rather than the proof.
	ExpectedShared bool
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
		ControlsRun:       checkControls(planted, report, verifyParsed.Findings, gateParsed.Findings),
	}

	return report, nil
}

// checkControls asks, for each planted violation, whether the comparison actually reported it.
//
// This is the end-to-end assertion and the reason the whole file exists. It deliberately looks in
// report.Differences rather than in either gate's raw findings: a control that one gate found but
// the comparison dropped is exactly the bug worth catching, and checking the raw output instead
// would sail straight past it.
func checkControls(planted []Control, report Report, verifyFindings []Finding, gateFindings []Finding) []ControlResult {
	results := make([]ControlResult, 0, len(planted))
	for _, control := range planted {
		result := ControlResult{
			Name:         control.Name,
			ExpectedSide: control.ExpectedSide,
			Rule:         control.Rule,
		}

		if control.ExpectedShared {
			results = append(results, checkSharedControl(control, result, verifyFindings, gateFindings))
			continue
		}

		for _, difference := range report.Differences {
			if !controlMatches(control, difference.Finding) {
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

// checkSharedControl asks whether both gates reported the planted finding.
//
// It reads the raw per-side findings rather than the comparison's output, because the property
// being tested is that the finding survived each side's parse and normalization and then matched.
// Asking the difference list could only ever say the control was absent from it, which is true both
// when both sides saw it and when neither did — the two outcomes this control exists to separate.
func checkSharedControl(control Control, result ControlResult, verifyFindings []Finding, gateFindings []Finding) ControlResult {
	seenByVerify := false
	for _, finding := range verifyFindings {
		if controlMatches(control, finding) {
			seenByVerify = true
			break
		}
	}
	seenByGate := false
	for _, finding := range gateFindings {
		if controlMatches(control, finding) {
			seenByGate = true
			break
		}
	}

	switch {
	case seenByVerify && seenByGate:
		result.Detected = true
	case !seenByVerify && !seenByGate:
		result.Detail = fmt.Sprintf("neither gate reported %s in %s, so the plant never reached either one", control.Rule, control.RelativePath)
	case seenByVerify:
		result.Detail = fmt.Sprintf("only verify reported %s in %s, and both were expected to", control.Rule, control.RelativePath)
	default:
		result.Detail = fmt.Sprintf("only the gate reported %s in %s, and both were expected to", control.Rule, control.RelativePath)
	}
	return result
}

// controlMatches is whether a finding is the one a control planted.
//
// Matched on rule and path suffix rather than on an exact path, because the two gates print paths
// differently and the parser normalizes them against the tree root, so the stored path is relative
// while the control names a relative path of its own.
func controlMatches(control Control, finding Finding) bool {
	if finding.Rule != control.Rule {
		return false
	}
	return strings.HasSuffix(filepath.ToSlash(finding.File), filepath.ToSlash(control.RelativePath))
}

// plantControls writes each control into the tree and returns a cleanup that removes them.
//
// It refuses to overwrite an existing file. On a tree with several authors in it, clobbering
// somebody's work to run a self-test would be a worse defect than the one the self-test is looking
// for, and a control that silently replaced a real file would also be linted for the wrong reasons.
func plantControls(options RunOptions) ([]Control, func(), error) {
	planted := make([]Control, 0, len(options.Controls))
	written := make([]string, 0, len(options.Controls))
	// createdDirectories is the directories this plant brought into existence, deepest first, so
	// cleanup can unwind exactly what it made. Removing the file and leaving its directory behind
	// is still a mutation of a tree several people are working in, and it shows up in their
	// `git status` as an unexplained empty directory. An earlier run of this harness did precisely
	// that and the leftover was found by someone else.
	createdDirectories := make([]string, 0, len(options.Controls))

	cleanup := func() {
		for _, path := range written {
			// A control left behind would be linted by the next person to run either gate, so a
			// failure to remove one is worth saying out loud rather than swallowing.
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "differential: could not remove planted control %s: %v\n", path, err)
			}
		}
		// Deepest first, and only the ones this plant created. Remove refuses on a non-empty
		// directory, which is the guard that matters: if anything else landed in there while the
		// gates ran, it stays and so does the directory.
		for index := len(createdDirectories) - 1; index >= 0; index-- {
			if err := os.Remove(createdDirectories[index]); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "differential: left the directory %s in place: %v\n", createdDirectories[index], err)
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
		missing, err := missingDirectories(filepath.Dir(absolutePath), options.Root)
		if err != nil {
			return planted, cleanup, fmt.Errorf("planning the directory for control %q: %w", control.Name, err)
		}
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			return planted, cleanup, fmt.Errorf("creating the directory for control %q: %w", control.Name, err)
		}
		createdDirectories = append(createdDirectories, missing...)
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

// missingDirectories lists the directories from root down to directory that do not exist yet.
//
// Asked before MkdirAll rather than inferred afterward, because afterward every directory on the
// path exists and there is no way to tell which ones were already there. Removing a directory the
// tree already had would be a worse mutation than leaving one behind.
//
// The walk stops at root and never above it, so a control cannot cause a directory outside the
// tree being linted to be removed. Ordered shallowest first; the caller unwinds in reverse.
func missingDirectories(directory string, root string) ([]string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}

	var missing []string
	for current := absoluteDirectory; strings.HasPrefix(current, absoluteRoot) && current != absoluteRoot; {
		if _, err := os.Stat(current); err == nil {
			break
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// Reverse into shallowest-first order, which is the order they get created in.
	for left, right := 0, len(missing)-1; left < right; left, right = left+1, right-1 {
		missing[left], missing[right] = missing[right], missing[left]
	}
	return missing, nil
}
