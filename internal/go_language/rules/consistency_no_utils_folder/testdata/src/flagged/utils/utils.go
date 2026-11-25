// want `^Folder name "utils" is not allowed\. Use "utilities" instead\.$`

// The folder and the package clause are both utils, and the file is reported once, for the folder, since renaming
// the folder is the moment to rename the clause with it.
package utils

// Parse is something the package holds.
func Parse(text string) string {
	return text
}
