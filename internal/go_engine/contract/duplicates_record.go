package contract

import (
	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// DuplicatesRecord is the duplicates report (#jfvhbj7): one record, after the unused phase's and before inputs, under
// `--duplicates` alone. It is this engine's, additive within contract 3, and never a finding: the summary does not
// count it and it never moves the exit code. Its fields are duplicate_functions.Report's, which the front door decodes
// and prints as it prints a TypeScript run's.
type DuplicatesRecord struct {
	duplicate_functions.Report
}

func (DuplicatesRecord) kind() Kind { return KindDuplicates }
