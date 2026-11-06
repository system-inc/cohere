package tailwind

import (
	"fmt"
	"sort"
	"strings"
)

// Report is one differential run: what it covered, what it found, and whether it can be trusted.
type Report struct {
	// TailwindVersion is the engine version the fixture was captured against.
	TailwindVersion string
	// GoSideName identifies the Go implementation under test.
	GoSideName string
	// Populations is one entry per design system, in fixture order.
	Populations []Population
	// Divergences is every non-agreeing class, controls included and marked.
	Divergences []Divergence
	// Controls is what each control did.
	Controls []ControlResult
	// SystemDivergence records the two design systems against each other rather than against Go.
	// See RepositoriesDiverge.
	SystemDivergence SystemDivergence
}

// ControlsProven is whether the harness was shown able to detect a difference at all.
//
// Reported separately from Trustworthy because they answer different questions, which is the
// distinction `internal/differential` draws and the one worth keeping. Trustworthy asks whether this
// run's population was real. This asks whether the instrument works.
//
// Every control must have fired, and they must cover both directions of silence. A harness that can
// only see Go-side silence reports a clean run for every class where Go invents a reading the engine
// never produced, and that direction is the one nothing else in the suite is looking at.
func (report Report) ControlsProven() bool {
	// Stated explicitly, though the loop below would reach the same answer: with no controls both
	// direction flags stay false and the final conjunction returns false anyway. Mutation-tested and
	// confirmed equivalent, so this line is documentation rather than logic, and it is kept because
	// "a run with no controls has proven nothing" is the single most important thing this function
	// says and it should not have to be derived from an empty loop.
	if len(report.Controls) == 0 {
		return false
	}
	sawGoSilentDirection := false
	sawEngineSilentDirection := false
	for _, result := range report.Controls {
		if !result.Detected {
			return false
		}
		switch result.Control.ExpectedOutcome {
		case OutcomeGoSilent:
			sawGoSilentDirection = true
		case OutcomeEngineSilent:
			sawEngineSilentDirection = true
		}
	}
	return sawGoSilentDirection && sawEngineSilentDirection
}

// Trustworthy reports whether this run's population was real, with the reasons it was not.
//
// Separate from whether it found anything, and checked first. Every reason below describes a run
// whose agreement numbers mean nothing, and the point of returning them as strings is that a caller
// prints them: a boolean false here reads as "the port is broken" when it means "this run did not
// measure anything".
func (report Report) Trustworthy() (bool, []string) {
	var reasons []string

	if len(report.Populations) < 2 {
		// One design system is not a population. Per-repository divergence is the defect the whole
		// port exists to fix, and it is unobservable from inside a single repository.
		reasons = append(reasons, fmt.Sprintf(
			"%d design systems measured, and per-repository divergence cannot be observed from one",
			len(report.Populations),
		))
	}

	for _, population := range report.Populations {
		if population.RegistryClasses < minimumPlausibleRegistryClasses {
			reasons = append(reasons, fmt.Sprintf(
				"%s: %d registry classes is below the plausible floor of %d, so the run was pointed at the wrong thing",
				population.SystemName, population.RegistryClasses, minimumPlausibleRegistryClasses,
			))
		}
		if population.CorpusClasses < minimumPlausibleCorpusClasses {
			reasons = append(reasons, fmt.Sprintf(
				"%s: %d corpus classes is below the plausible floor of %d, and the corpus is where the engine's null readings live",
				population.SystemName, population.CorpusClasses, minimumPlausibleCorpusClasses,
			))
		}
		if population.Answered() == 0 {
			reasons = append(reasons, fmt.Sprintf(
				"%s: no class was answered by both sides, so there is nothing to agree about",
				population.SystemName,
			))
		}
		if total := population.Total(); total > 0 {
			bothSilentRate := float64(population.ByOutcome[OutcomeBothSilent]) / float64(total)
			if bothSilentRate > maximumPlausibleBothSilentRate {
				reasons = append(reasons, fmt.Sprintf(
					"%s: %d of %d classes (%.1f%%) had neither side answer, above the %.0f%% ceiling \u2014 a side reporting nothing is a failed run",
					population.SystemName, population.ByOutcome[OutcomeBothSilent], total,
					bothSilentRate*100, maximumPlausibleBothSilentRate*100,
				))
			}
		}
		if population.UnknownPopulationClasses > 0 {
			reasons = append(reasons, fmt.Sprintf(
				"%s: %d classes named a population this package does not know, so the registry and corpus floors were measured against the wrong counts",
				population.SystemName, population.UnknownPopulationClasses,
			))
		}
		if population.EngineNullReadings == 0 {
			// The engine declines classes in both design systems and always has. Zero means the
			// fixture stopped carrying the classes it declines, which removes exactly the population
			// this harness was built to keep honest, and it removes it invisibly.
			reasons = append(reasons, fmt.Sprintf(
				"%s: the engine declined no class at all, so the population that proves silence is not scored has gone missing",
				population.SystemName,
			))
		}
	}

	if !report.ControlsProven() {
		reasons = append(
			reasons,
			"no control fired in both silent directions, so the harness has not been shown able to detect a difference",
		)
	}
	for _, result := range report.Controls {
		if result.Detected {
			continue
		}
		reasons = append(reasons, fmt.Sprintf(
			"control %q did not fire: %s \u2014 the harness has not been shown able to detect a difference",
			result.Control.Name, result.Detail,
		))
	}

	return len(reasons) == 0, reasons
}

// RepositoriesDiverge reports whether the two design systems differ at all, and how.
//
// This is `f7d1d8d`'s discipline: take the claim the port rests on and make it an assertion that
// fails if it ever becomes trivially true. The claim is that a table generated from one repository
// is wrong for another.
//
// Measured, the divergence is not where reading the theme result suggests. Theme resolution diverges
// on *values*, 744 entries against 748 with 22 shared keys carrying different values, and a reading
// is `{order, count}` where order indexes the declared *property*. A shared class declaring the same
// property with a different value therefore reads identically in both, and all 95,134 shared
// registry classes do.
//
// The divergence is in *membership*: 462 classes exist in exactly one system, because a
// `--color-brand-*` token in one repository's `@theme` is what makes `accent-brand` a class at all.
// A table generated against ahra has no row answering `accent-brand`, so it declines a class the
// engine compiles. That is a silent-side defect rather than a wrong reading, and it is the sharper
// statement of why a per-repository table is required.
//
// Both axes are returned so that a future Tailwind which starts diverging readings shows up as a
// number moving off zero rather than as a fixture that quietly still passes.
func (report Report) RepositoriesDiverge() (bool, string) {
	if report.SystemDivergence.UniqueToOneSystem == 0 && report.SystemDivergence.DivergentReadings == 0 {
		return false, fmt.Sprintf(
			"the two design systems agree on all %d shared registry classes and neither holds a class the other "+
				"lacks, so this fixture no longer demonstrates that a per-repository table is required",
			report.SystemDivergence.SharedRegistryClasses,
		)
	}
	return true, fmt.Sprintf(
		"%d shared registry classes read identically, %d read differently, and %d classes exist in only one system",
		report.SystemDivergence.SharedRegistryClasses,
		report.SystemDivergence.DivergentReadings,
		report.SystemDivergence.UniqueToOneSystem,
	)
}

// Describe renders the run as prose a reader can check, controls separated from observations.
func (report Report) Describe() string {
	builder := &strings.Builder{}

	fmt.Fprintf(builder, "tailwind %s, go side %q\n", report.TailwindVersion, report.GoSideName)

	for _, population := range report.Populations {
		fmt.Fprintf(builder,
			"  %s: %d registry + %d corpus classes, %d answered by both\n",
			population.SystemName, population.RegistryClasses, population.CorpusClasses, population.Answered(),
		)
		fmt.Fprintf(builder,
			"    agreed %d, disagreed %d, go-silent %d, engine-silent %d, both-silent %d (engine declined %d)\n",
			population.ByOutcome[OutcomeAgreed], population.ByOutcome[OutcomeDisagreed],
			population.ByOutcome[OutcomeGoSilent], population.ByOutcome[OutcomeEngineSilent],
			population.ByOutcome[OutcomeBothSilent], population.EngineNullReadings,
		)
		if answered := population.Answered(); answered > 0 {
			fmt.Fprintf(builder, "    agreement over answered classes: %.4f%%\n",
				float64(population.ByOutcome[OutcomeAgreed])/float64(answered)*100)
		}
	}

	// Observed and planted are counted apart, never summed. A run with three controls reporting
	// "3 divergences" on a design system whose real gap was zero is a sentence a reader takes as
	// remaining exposure, and that has happened in this repository.
	observed, planted := 0, 0
	for _, divergence := range report.Divergences {
		if divergence.Planted {
			planted++
			continue
		}
		observed++
	}
	fmt.Fprintf(builder, "  divergences: %d observed, %d planted by controls\n", observed, planted)

	for _, result := range report.Controls {
		status := "did not fire"
		if result.Detected {
			status = "fired"
		}
		fmt.Fprintf(builder, "    control %-22s %s: %d detections (floor %d) \u2014 %s\n",
			result.Control.Name, status, result.Detections, result.Control.MinimumDetections, result.Control.Detail)
	}

	_, divergenceDetail := report.RepositoriesDiverge()
	fmt.Fprintf(builder, "  design systems: %s\n", divergenceDetail)

	return builder.String()
}

// DivergencesByOutcome groups the observed divergences, sorted, for a report a reader can act on.
//
// Planted divergences are excluded. They are proof the instrument works and they are not findings,
// and a list that mixed them would hand a reader control noise to triage.
func (report Report) DivergencesByOutcome() map[Outcome][]Divergence {
	grouped := map[Outcome][]Divergence{}
	for _, divergence := range report.Divergences {
		if divergence.Planted {
			continue
		}
		grouped[divergence.Outcome] = append(grouped[divergence.Outcome], divergence)
	}
	for outcome := range grouped {
		sort.Slice(grouped[outcome], func(left, right int) bool {
			return grouped[outcome][left].ClassName < grouped[outcome][right].ClassName
		})
	}
	return grouped
}
