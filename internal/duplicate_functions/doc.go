// Package duplicate_functions is the language-neutral half of `cohere --duplicates` (#jfvhbj7): the canonical digest
// both engines hash a function into, the records a hashed or refused function leaves, and the report that groups
// functions whose digests are equal into one idea written more than once.
//
// Each engine owns how it reads a function: TypeScript lowers it to the high-level intermediate representation
// (internal/lint/ecmascript/canonical_function_hash), Go reads staticcheck's intermediate representation
// (internal/go_engine/canonical_function_hash). Both build the Report here, and the front door prints it, so a
// duplicate reads the same whichever engine found it.
//
// The report is a report, not a gate (@system_cohere's ruling on #6rj7jw7): nothing here is a finding, and a run's
// exit code never depends on it.
package duplicate_functions
