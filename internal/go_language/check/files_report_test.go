package check

// filesReport is filesReportAnalyzer's result on a package: its path and how many files its pass was given, exported
// so the findings cache can encode it.
type filesReport struct {
	Path  string
	Files int
}
