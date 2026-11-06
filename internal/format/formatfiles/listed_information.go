package formatfiles

import (
	"os"
	"time"
)

// listedInformation is a listed entry as the walk's callback reads it: its name, whether it is a directory and
// its type bits. Size and modification time are not in a listing, so both are zero, and Enumerate's callback
// must never read them: from Walk it would get the true values and from a listed walk zero, silently.
type listedInformation struct {
	entry os.DirEntry
}

func (information listedInformation) Name() string { return information.entry.Name() }

func (information listedInformation) Size() int64 { return 0 }

func (information listedInformation) Mode() os.FileMode { return information.entry.Type() }

func (information listedInformation) ModTime() time.Time { return time.Time{} }

func (information listedInformation) IsDir() bool { return information.entry.IsDir() }

func (information listedInformation) Sys() any { return nil }
