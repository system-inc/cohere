package program

import (
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// checkedStatsFileSystem answers existence checks and stats from the run cache's check where it statted the path, and
// asks the disk beneath for everything else. It answers as the disk does (vfs's Common): a stat follows a
// symbolic link, a file exists when something that is not a directory is there, and a directory when a directory
// is. Listings, reads and realpaths pass through.
type checkedStatsFileSystem struct {
	vfs.FS
	snapshot *StatSnapshot
	answered *atomic.Int64
}

func (fileSystem *checkedStatsFileSystem) FileExists(path tspath.RootedFilePath) bool {
	if information, present, known := fileSystem.snapshot.answer(path.AsString()); known {
		fileSystem.answered.Add(1)
		return present && !information.IsDir()
	}
	return fileSystem.FS.FileExists(path)
}

func (fileSystem *checkedStatsFileSystem) DirectoryExists(path tspath.RootedDirectoryPath) bool {
	if information, present, known := fileSystem.snapshot.answer(path.AsString()); known {
		fileSystem.answered.Add(1)
		return present && information.IsDir()
	}
	return fileSystem.FS.DirectoryExists(path)
}

func (fileSystem *checkedStatsFileSystem) Stat(path tspath.RootedPath) vfs.FileInfo {
	if information, present, known := fileSystem.snapshot.answer(path.AsString()); known {
		fileSystem.answered.Add(1)
		if !present {
			return nil
		}
		return information
	}
	return fileSystem.FS.Stat(path)
}
