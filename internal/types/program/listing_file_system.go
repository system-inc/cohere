package program

import (
	"io/fs"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// listingFileSystem serves GetAccessibleEntries from the listings and everything else from the disk beneath.
type listingFileSystem struct {
	vfs.FS
	listings *DirectoryListings
}

func (fileSystem *listingFileSystem) GetAccessibleEntries(directory tspath.RootedDirectoryPath) vfs.Entries {
	entries, listed := fileSystem.listings.Listing(directory.AsString())
	if !listed {
		return fileSystem.FS.GetAccessibleEntries(directory)
	}
	// What the real disk returns for the same entries (vfs/internal Common.GetAccessibleEntries).
	result := vfs.Entries{Symlinks: map[string]struct{}{}}
	add := func(name string, mode fs.FileMode, isLink bool) bool {
		switch {
		case mode.IsDir():
			result.Directories = append(result.Directories, name)
		case mode.IsRegular():
			result.Files = append(result.Files, name)
		default:
			return false
		}
		if isLink {
			result.Symlinks[name] = struct{}{}
		}
		return true
	}
	for _, entry := range entries {
		if add(entry.Name(), entry.Type(), false) {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if information := fileSystem.FS.Stat(
				tspath.RootedPath(directory.AsString() + "/" + entry.Name()),
			); information != nil {
				add(entry.Name(), information.Mode(), true)
			}
		}
	}
	return result
}
