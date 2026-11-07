package main

// counts is the measure the rule is about. A file serves a type when it declares the type and the type
// counts, or when it writes methods of a type that counts (ruling 13 (c)). A type counts when the build
// compiles a method of it and not every such method is a marker (isMarkerOnly).
type counts struct {
	typesWithMethods           int
	filesServingSeveral        int
	typesInFilesServingSeveral int
	// extraTypes is one per served type after a judged file's first, which is the one type per file
	// rule's finding count.
	extraTypes int
	// misnamed is the files the file name rule flags.
	misnamed                  int
	filesOverLineLimit        int
	pinnedFiles               int
	pinnedFilesServingSeveral int
	ambiguousTypesWithMethods int
}

func (total *counts) add(other counts) {
	total.typesWithMethods += other.typesWithMethods
	total.filesServingSeveral += other.filesServingSeveral
	total.typesInFilesServingSeveral += other.typesInFilesServingSeveral
	total.extraTypes += other.extraTypes
	total.misnamed += other.misnamed
	total.filesOverLineLimit += other.filesOverLineLimit
	total.pinnedFiles += other.pinnedFiles
	total.pinnedFilesServingSeveral += other.pinnedFilesServingSeveral
	total.ambiguousTypesWithMethods += other.ambiguousTypesWithMethods
}
