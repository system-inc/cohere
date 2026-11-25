package global_rename

import "strings"

// platformFile is one file only another platform builds, and what its identifiers resolve to under each
// platform that builds it.
type platformFile struct {
	filename    string
	relative    string
	packagePath string
	editable    bool
	readings    []platformFileReading
	// spellings counts each name's identifiers in the file.
	spellings map[string]int
}

// platformNames lists the platforms that read a file, for a reason.
func (file *platformFile) platformNames() string {
	names := make([]string, 0, len(file.readings))
	for _, fileReading := range file.readings {
		names = append(names, fileReading.platform.String())
	}
	return strings.Join(names, " and ")
}
