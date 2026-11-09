package format

import "fmt"

// OutsideRootError is a write refused because the file, every symbolic link resolved, is outside the run's root
// (WriteRoot): the run reached it through a link, and a fix of the project must not write past it.
type OutsideRootError struct {
	// File is the file as the run named it, and RealPath where it really is.
	File     string
	RealPath string
}

func (outside *OutsideRootError) Error() string {
	return fmt.Sprintf("%s is reached through a link to %s, outside the run's root", outside.File, outside.RealPath)
}
