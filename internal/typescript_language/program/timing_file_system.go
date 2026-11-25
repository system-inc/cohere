package program

import (
	"sync/atomic"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// timingFileSystem times the calls beneath it into whichever counters are current. The build switches them
// from the config's to the program's between the two, which is safe because both run on the build's
// own goroutine in order: no config call is still in flight when the program starts.
type timingFileSystem struct {
	vfs.FS
	current atomic.Pointer[diskCounters]
}

func (fileSystem *timingFileSystem) start() (*diskCounters, time.Time) {
	return fileSystem.current.Load(), time.Now()
}

func (fileSystem *timingFileSystem) ReadFile(path tspath.RootedFilePath) (string, bool) {
	counters, started := fileSystem.start()
	contents, ok := fileSystem.FS.ReadFile(path)
	counters.add(diskRead, started)
	return contents, ok
}

func (fileSystem *timingFileSystem) FileExists(path tspath.RootedFilePath) bool {
	counters, started := fileSystem.start()
	exists := fileSystem.FS.FileExists(path)
	counters.add(diskExistence, started)
	return exists
}

func (fileSystem *timingFileSystem) DirectoryExists(path tspath.RootedDirectoryPath) bool {
	counters, started := fileSystem.start()
	exists := fileSystem.FS.DirectoryExists(path)
	counters.add(diskExistence, started)
	return exists
}

func (fileSystem *timingFileSystem) Stat(path tspath.RootedPath) vfs.FileInfo {
	counters, started := fileSystem.start()
	information := fileSystem.FS.Stat(path)
	counters.add(diskStat, started)
	return information
}

func (fileSystem *timingFileSystem) GetAccessibleEntries(path tspath.RootedDirectoryPath) vfs.Entries {
	counters, started := fileSystem.start()
	entries := fileSystem.FS.GetAccessibleEntries(path)
	counters.add(diskListing, started)
	return entries
}

func (fileSystem *timingFileSystem) Realpath(path tspath.RootedPath) tspath.RootedPath {
	counters, started := fileSystem.start()
	resolved := fileSystem.FS.Realpath(path)
	counters.add(diskRealpath, started)
	return resolved
}
