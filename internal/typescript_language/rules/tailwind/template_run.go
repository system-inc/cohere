package tailwind

import (
	"github.com/microsoft/TypeScript/tsc/shim/core"
)

// TemplateRun is one static run of a template with holes and the classes in it, for a rule that judges
// classes in pairs: upstream reads each piece of a template as a literal of its own, so two classes
// pair only within one run, never across a hole.
type TemplateRun struct {
	// Range is the run's own range, where a finding about several of its classes sits, as a literal's
	// sits on the literal.
	Range   core.TextRange
	Classes []TemplateClass
}

// TemplateRunsOf lists the runs of a template with holes that hold at least one class, in source order.
func TemplateRunsOf(sourceText string, segments []ClassSegment) []TemplateRun {
	runs := []TemplateRun{}
	for _, class := range TemplateClassesOf(sourceText, segments) {
		if len(runs) == 0 || runs[len(runs)-1].Classes[0].Run != class.Run {
			runs = append(runs, TemplateRun{Range: segments[class.Run].Range})
		}
		runs[len(runs)-1].Classes = append(runs[len(runs)-1].Classes, class)
	}
	return runs
}

// Texts is the run's classes as written, in order.
func (run TemplateRun) Texts() []string {
	texts := make([]string, len(run.Classes))
	for index, class := range run.Classes {
		texts[index] = class.Text
	}
	return texts
}
