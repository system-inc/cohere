// Command verify-differential runs both gates over one tree and reports where they disagree.
//
// This is the acceptance instrument for the whole project. verify replaces the gate only when it
// can be shown to find what the gate finds and stay silent where the gate is silent, and until this
// command existed that claim was established by a person running two commands and comparing counts
// by eye — which is not a harness, does not survive that person, and cannot answer the question it
// appears to answer.
//
// The question it appears to answer is "do the two gates agree." The question it has to answer
// first is "did both gates run, and can this harness see a difference at all." Those are not the
// same, and an empty diff satisfies the first while proving nothing about the second. So this
// command plants a known violation on every run and refuses a verdict when the plant did not come
// out the far end.
//
//	verify-differential                          compare, planting the default controls
//	verify-differential -root ~/Projects/ahra    the tree to compare over
//	verify-differential -verify-binary <path>    which verify to measure
//	verify-differential -no-controls             skip planting, and say so in the report
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/system-inc/verify/internal/config"
	"github.com/system-inc/verify/internal/differential"
	"github.com/system-inc/verify/internal/registry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "verify-differential: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	options, err := parseArguments(os.Args[1:])
	if err != nil {
		return err
	}

	lintConfig, err := config.Load(filepath.Join(options.root, ".oxlintrc.json"))
	if err != nil {
		return fmt.Errorf("loading the lint config: %w", err)
	}

	if err := checkControlsAreLintable(lintConfig, options.controls, verifyOnlyRuleSettings(options.root)); err != nil {
		return err
	}

	report, err := differential.Run(context.Background(), differential.RunOptions{
		Root:                    options.root,
		Verify:                  options.verifyCommand,
		Gate:                    options.gateCommand,
		VerifyRules:             compiledRuleNames(),
		ConfiguredRules:         configuredRuleNames(lintConfig),
		Controls:                options.controls,
		ExtraVerifyRuleSettings: verifyOnlyRuleSettings(options.root),
	})
	if err != nil {
		return err
	}

	rendered := &strings.Builder{}
	differential.Write(rendered, report)
	fmt.Print(rendered.String())

	// The exit code comes from the same value the reader sees rather than being recomputed, so the
	// two cannot disagree. Agreed() is false for a disagreement and equally false for a run that
	// proved nothing, which is correct: neither is a result anyone should build on.
	if !report.Agreed() {
		os.Exit(1)
	}
	return nil
}

// compiledRuleNames is every rule actually linked into this binary.
//
// Read from the registry rather than from a list, because a list is a claim about the binary and
// the registry is the binary. During the migration this tool replaces, three configurations ran
// successfully having loaded zero plugins.
func compiledRuleNames() map[string]bool {
	names := map[string]bool{}
	for _, compiled := range registry.All() {
		names[differential.NormalizeRuleName(compiled.Name)] = true
	}
	return names
}

// configuredRuleNames is every rule the lint config switches on.
//
// The config keys carry their plugin prefix (`nexus/consistency-no-enum`) and verify reports bare
// names, so both are normalized to the same form here. Skipping that would make every configured
// rule look unconfigured, and every real disagreement would then be filed as not-configured and
// silently excused.
//
// Rules set to off are excluded rather than counted, because a rule the config turned off ran over
// no files, which is exactly what not-configured means.
func configuredRuleNames(lintConfig *config.Config) map[string]bool {
	names := map[string]bool{}
	for name, setting := range lintConfig.Rules {
		if setting.Severity == config.SeverityOff {
			continue
		}
		names[differential.NormalizeRuleName(name)] = true
	}
	return names
}

type arguments struct {
	root          string
	verifyCommand differential.GateCommand
	gateCommand   differential.GateCommand
	controls      []differential.Control
}

func parseArguments(rest []string) (arguments, error) {
	parsed := arguments{root: defaultRoot}
	verifyBinary := "verify"
	plantControls := true

	for index := 0; index < len(rest); index++ {
		switch rest[index] {
		case "-root", "--root":
			if index+1 >= len(rest) {
				return parsed, fmt.Errorf("-root needs a directory")
			}
			index++
			parsed.root = rest[index]
		case "-verify-binary", "--verify-binary":
			if index+1 >= len(rest) {
				return parsed, fmt.Errorf("-verify-binary needs a path")
			}
			index++
			verifyBinary = rest[index]
		case "-no-controls", "--no-controls":
			plantControls = false
		default:
			return parsed, fmt.Errorf("unknown argument %q", rest[index])
		}
	}

	absoluteRoot, err := filepath.Abs(parsed.root)
	if err != nil {
		return parsed, fmt.Errorf("resolving the root %s: %w", parsed.root, err)
	}
	parsed.root = absoluteRoot

	parsed.verifyCommand = differential.GateCommand{
		Name:      "verify",
		Program:   verifyBinary,
		Arguments: []string{"-lint"},
		Directory: parsed.root,
	}
	parsed.gateCommand = differential.GateCommand{
		Name:      "gate",
		Program:   "node",
		Arguments: []string{gateRunnerPath, "--no-cache"},
		Directory: parsed.root,
	}

	if plantControls {
		parsed.controls = defaultControls()
	}

	return parsed, nil
}

// defaultControls are the planted violations that prove the pipeline carries a difference.
//
// # Why there is a shared control and not a verify-only one
//
// The first version of this planted an enum and expected verify alone to see it. It missed, and the
// miss was correct: `consistency-no-enum` is implemented on both sides and enabled in the config, so
// both gates saw the plant and it matched. The control was wrong, not the harness — and the harness
// caught it, refused a verdict, and printed the difference list as not-a-measurement, which is the
// behavior it was built for.
//
// Chasing that further produced a more useful conclusion than a working control would have. There
// is no verify-only control available on this tree today, and the reason is structural rather than
// an oversight:
//
//   - Every rule verify implements was ported from the gate, so both sides have it.
//   - A rule in verify but absent from the config runs over no files in verify either, so it fires
//     on neither side.
//   - Verify honors `ignorePatterns` and the config overrides, so a plant in an ignored path or
//     under a scoping override is correctly silent on both sides.
//
// A verify-only difference can therefore only arise from a genuine behavioral divergence, and one
// cannot be planted to order without first knowing of a real one. That is a good property of a
// faithful port and a real limit on this instrument, so it is written here rather than worked
// around with a control that tests something other than what it claims.
//
// What is planted instead is a shared control: a violation both gates must see. It does not prove
// direction, and `ControlsProven` still returns false because of that. It does prove the pipeline
// end to end — process launched, output parsed, paths normalized, rule names collapsed across two
// decoration schemes, comparison run — which is the layer where a silent total mismatch lives. A
// plant that both sides report as one shared finding, rather than as two one-sided ones, is
// positive evidence that path normalization and rule-name normalization both worked.
//
// The gate-only direction needs no plant: the gate reports findings from rules verify has not
// ported, so that direction has a natural population on this tree.
func defaultControls() []differential.Control {
	return []differential.Control{{
		Name:         "shared-enum",
		RelativePath: filepath.Join("code-quality", "differential-control", "PlantedEnum.ts"),
		Contents: "// Planted by verify-differential to prove the harness can see a difference.\n" +
			"// Removed automatically when the run finishes. If you are reading this in a working\n" +
			"// tree, a differential run was interrupted and this file is safe to delete.\n" +
			"export enum PlantedControlOrder {\n    First = 'First',\n}\n",
		Rule: "consistency-no-enum",
		// Both gates implement and enable this rule, so both must see it. Expecting it shared is
		// the claim being tested; expecting it one-sided would be testing a divergence that does
		// not exist.
		ExpectedShared: true,
	}, {
		// The verify-only control. Verified against the tree at 03:17 rather than assumed:
		//
		//   registry.go:52                 verify has import-require-path-alias compiled in
		//   NexusLintConfiguration.ts:31   the ESLINT plugin defines it
		//   OxlintNexusPlugin.mjs          the OXLINT plugin does not, at all
		//   .oxlintrc.json                 no mention of the rule
		//
		// The gate we diff against runs oxlint, so it is structurally incapable of producing this
		// finding, and no configuration change on that side can make it. That is what makes this a
		// sturdy directional control rather than one resting on a defect somebody might repair: a
		// control built on a bug stops discriminating the moment the bug is fixed.
		//
		// The rule needs its options, because the shared config does not enable it and verify runs
		// an unconfigured rule over no files. It is marked Required in the registry, so an
		// unconfigured run refuses loudly instead of coming back clean, and the harness cannot get
		// a false pass from forgetting this.
		Name:         "verify-only-path-alias",
		RelativePath: filepath.Join("code-quality", "differential-control", "deep", "nested", "PlantedImport.ts"),
		Contents: `// Planted by verify-differential to prove the harness can see a one-sided
// difference. Removed automatically when the run finishes.
import { PlantedTarget } from '../../target/PlantedTarget';

export const PlantedUse = PlantedTarget;
`,
		Rule:         "import-require-path-alias",
		ExpectedSide: differential.SideVerify,
	}, {
		// The target of the planted import. It exists so the import resolves and so the rule has an
		// aliased directory to climb into; it carries no violation of its own and is expected to
		// produce nothing, which is why it declares no Rule and no side.
		Name:         "verify-only-path-alias-target",
		RelativePath: filepath.Join("code-quality", "differential-control", "target", "PlantedTarget.ts"),
		// PascalCase because it is exported: the tree's own constant-casing rule flags an exported
		// camelCase constant, and a support file that trips a rule stops being support. The first
		// version was `plantedTarget` and it produced a real finding of its own, which is exactly
		// the measurement perturbation these files must not cause.
		Contents:       "export const PlantedTarget = 'planted';\n",
		ExpectsNothing: true,
	}, {
		// The gate-only control, and the mirror image of the verify-only one. Verified at 03:21
		// rather than assumed:
		//
		//   registry.go             verify does not implement consistency-organize-imports at all
		//   .oxlintrc.json          structure/consistency-organize-imports is enabled
		//
		// So the gate reports it and verify structurally cannot, for the same durable reason and in
		// the opposite direction. Together the two controls exercise both sides, which is what
		// ControlsProven has been waiting on all night.
		//
		// The violation is import ordering: a local import placed above a node: one, which the
		// gate's rule flags and verify has no opinion about.
		Name:         "gate-only-organize-imports",
		RelativePath: filepath.Join("code-quality", "differential-control", "PlantedOrder.ts"),
		Contents: `// Planted by verify-differential to prove the harness can see a one-sided
// difference from the gate. Removed automatically when the run finishes.
import { PlantedTarget } from './target/PlantedTarget';
import * as NodePath from 'node:path';

export const PlantedOrder = NodePath.join(PlantedTarget);
`,
		Rule:         "consistency-organize-imports",
		ExpectedSide: differential.SideGate,
	}}
}

// verifyOnlyRuleSettings enables, for verify's run alone, the rules the gate cannot express.
//
// Only rules absent from the gate's plugin belong here. A rule both sides implement must stay on
// the shared config, because configuring it differently per side manufactures differences that say
// nothing about either implementation. `Run` refuses a rule the tree's config already configures,
// which is the mechanical version of that boundary rather than a comment asking for care.
func verifyOnlyRuleSettings(root string) map[string]any {
	// The alias is scoped to the control's own directory rather than to a real source root.
	//
	// A first version aliased `modules`, which is a real directory, and the rule fired 95 times
	// across the tree. Every one of those was a true finding about code nobody has asked this rule
	// to judge, and they drowned the single finding the control exists to produce. A control must
	// perturb the measurement by exactly the one finding it plants; anything else is the
	// instrument changing what it measures.
	//
	// Aliasing the control directory means the only relative import that can climb into an aliased
	// directory is the one in the planted file.
	return map[string]any{
		"nexus/import-require-path-alias": []any{
			"error",
			map[string]any{
				"repositoryRoot": root,
				"aliases": []any{
					map[string]any{
						"directory": filepath.Join("code-quality", "differential-control"),
						"alias":     "@differential-control",
					},
				},
			},
		},
	}
}

// gateRunnerPath is the gate as it actually ships, run through its own cached runner rather than by
// invoking oxlint directly. Comparing against a hand-rolled oxlint invocation would measure a gate
// nobody runs.
var gateRunnerPath = filepath.Join(
	"libraries", "structure", "libraries", "nexus", "code-quality", "oxlint", "RunCachedOxlint.ts",
)

// defaultRoot is the tree this instrument was commissioned to measure.
var defaultRoot = filepath.Join(os.Getenv("HOME"), "Projects", "ahra")

// checkControlsAreLintable refuses to run when a control was placed where its rule cannot fire.
//
// A control exists to prove the harness can see a difference, so a control that misses is supposed
// to mean the harness is blind. But a control written to a path the config ignores, or naming a
// rule the config never enables, misses for a reason that has nothing to do with the harness and
// produces exactly the same output. The evidence and the defect are indistinguishable, which makes
// the control worse than no control: it reports a failure the reader will attribute to the wrong
// thing.
//
// So the precondition is checked against the config rather than assumed from the path, and it is a
// refusal to start rather than a note in the report. The comment on Control.RelativePath already
// said the path must be one both gates lint. A comment is carefully safe; this is mechanically
// safe, and mechanical is the kind that survives the next editor.
//
// The check runs before either gate is launched, so a misplaced control costs a second rather than
// two full lint runs and a misleading verdict.
func checkControlsAreLintable(lintConfig *config.Config, controls []differential.Control, extraVerifyRules map[string]any) error {
	// The rules supplied to verify alone, reduced to bare names. A control naming one of these is
	// enabled for verify's run even though the tree's own config says nothing about it, so the
	// check below has to know about them or it rejects exactly the directional control it should
	// be admitting. The ignore check still applies to them: an ignored path is ignored by both
	// sides regardless of which config names the rule.
	suppliedToVerify := map[string]bool{}
	for name := range extraVerifyRules {
		suppliedToVerify[differential.NormalizeRuleName(name)] = true
	}

	for _, control := range controls {
		// A support file asserts no finding, so there is no rule to check it can fire. The ignore
		// check below would also be meaningless for it: whether the tree lints it changes nothing
		// about what it proves, which is nothing.
		if control.ExpectsNothing {
			continue
		}

		resolved := lintConfig.Resolve(filepath.Join(lintConfig.Root, control.RelativePath))

		if resolved.Ignored {
			return fmt.Errorf(
				"control %q is placed at %s, which the lint config ignores via %q, so it would miss for a reason unrelated to the harness; move it somewhere both gates lint",
				control.Name, control.RelativePath, resolved.IgnoredBy,
			)
		}

		// The rule has to be enabled for that specific path, not merely present in the config: an
		// override can scope a rule off for exactly the directory a control was written to, and
		// that override is invisible from the base rule list.
		if !resolved.Enabled(pluginQualified(lintConfig, control.Rule)) && !suppliedToVerify[control.Rule] {
			return fmt.Errorf(
				"control %q expects rule %s to fire at %s, but the lint config does not enable it there, so the control would miss for a reason unrelated to the harness",
				control.Name, control.Rule, control.RelativePath,
			)
		}
	}
	return nil
}

// pluginQualified finds the config's own spelling of a bare rule name.
//
// The config keys rules as `plugin/rule-name` and a control names the bare rule, so a direct lookup
// would miss every time and this guard would reject every control. Searching for the key whose
// normalized form matches keeps the control declarations free of plugin prefixes, which is the
// right place for that knowledge to live: a rule that moves between plugins should not break a
// control that never mentioned one.
//
// Returning the bare name when nothing matches is deliberate. It makes Enabled report unconfigured,
// which is the correct answer for a rule the config genuinely does not mention, and it keeps this
// helper from being the thing that decides a control is valid.
func pluginQualified(lintConfig *config.Config, bareRuleName string) string {
	for name := range lintConfig.Rules {
		if differential.NormalizeRuleName(name) == bareRuleName {
			return name
		}
	}
	return bareRuleName
}
