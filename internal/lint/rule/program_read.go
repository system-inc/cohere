package rule

import "strings"

// ProgramRead is what a rule reads through ctx.Program beyond the file it was handed, declared so the
// findings cache can tell which reads its key already covers (#b8k3bp6).
//
// It replaced one flag, ReadsProgram, that said "reaches the program" and so made 45 rules uncacheable,
// about a third of the rule time on ahra, when most of them read nothing the key does not already
// hold. The kinds are split by what covers them:
//
//	ReadsCompilerOptions   the tsconfig's options and the run's directory: the findings key hashes
//	                       the tsconfig chain and the root
//	ReadsDefaultLibrary    whether a declaration lives in a lib file: the libs are embedded in the
//	                       binary, which is in the key
//	ReadsModuleResolution  where this file's own imports resolve: the type fingerprint's edges come
//	                       from resolution, and every file it can reach is in the closure or the
//	                       global component
//	ReadsOtherFiles        anything else: the program's file list, a file by name or by resolution,
//	                       another file's imports, the file system. Nothing per file covers it, so a
//	                       rule declaring it is never cached
//	ReadsDesignSystem      the files the Tailwind design system is built from: its entry point, the
//	                       stylesheets it imports, and the paths it probed and did not find. Read
//	                       through DesignSystemFileSystem, which records every one, so the set is observed
//	                       rather than listed and can key a cache of its own (#pyhm2t2)
//
// A rule reads ctx.Program through a view that refuses every method outside its declaration, with a
// panic naming the rule and the read. The walk contains that panic to the rule and names it, so a rule
// cannot read more than it declared without every run, fixture and crash guard saying so. Declaring
// more than a rule reads costs a cache miss and nothing else, the same asymmetry NeedsTypeChecker has.
type ProgramRead uint8

// String lists the reads a declaration holds, for refusals and tests.
func (reads ProgramRead) String() string {
	names := []string{}
	for _, read := range []ProgramRead{
		ReadsCompilerOptions,
		ReadsDefaultLibrary,
		ReadsModuleResolution,
		ReadsOtherFiles,
		ReadsDesignSystem,
	} {
		if reads&read != 0 {
			names = append(names, programReadNames[read])
		}
	}
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, " | ")
}
