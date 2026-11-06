// want `^Folder name "utils" is not allowed\. Use "utilities" instead\.$`

// A second file of the package carries a finding of its own, since the finding is once per file.
package utils

// Format is something else the package holds.
func Format(text string) string {
	return text
}
