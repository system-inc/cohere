// The folder and the package clause are both utils, and the package is reported once, for the folder, since renaming
// the folder is the moment to rename the clause with it.
package utils // want `^Folder name "utils" is not allowed\. Use "utilities" instead\.$`

// Parse is something the package holds.
func Parse(text string) string {
	return text
}
