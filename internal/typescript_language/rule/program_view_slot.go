package rule

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
)

// ProgramViewSlot is one rule's view of the program, kept by a walk worker and pointed at each file it
// dispatches in turn. The walk used to build every rule's view afresh for each file, 101 MB of views on a
// cold ahra run (#9xfg09f); a slot per rule per worker is a few hundred kilobytes for the whole run.
//
// A view is good for its file and no longer. Release ends it, and from then on every read through it
// panics, naming the rule. The slot alternates between two views, so a Program a rule kept from one file
// is still the ended view while the next file runs, and reading it fails loudly rather than reading through
// whatever file the slot was pointed at next. A view kept across two or more files is not caught. What it
// would read is the same program under the same rule's declared reads, so only ResolveModule's "is this
// another file" check could answer differently.
type ProgramViewSlot struct {
	views [2]programView
	next  int
}

// Point aims the slot at sourceFile for subject and returns the view as the rule's Program. A nil program
// is a nil Program, as ViewProgram's is.
func (slot *ProgramViewSlot) Point(program *compiler.Program, sourceFile *ast.SourceFile, subject Traits) Program {
	if program == nil {
		return nil
	}
	view := &slot.views[slot.next]
	slot.next = 1 - slot.next
	*view = programView{program: program, sourceFile: sourceFile, ruleName: subject.Name, reads: subject.ProgramReads}
	return view
}

// Release ends the file the slot was last pointed at: every read through that view panics from now on.
func (slot *ProgramViewSlot) Release() {
	view := &slot.views[1-slot.next]
	if view.program == nil {
		return
	}
	view.ended = true
	view.program = nil
}
