package go_listing

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// A FileList is one of the lists of file names go list prints for a package. Its value is go list's
// name for the list, the one a -json field list takes.
type FileList string

// Every file list a Package decodes.
const (
	GoFiles           FileList = "GoFiles"
	CgoFiles          FileList = "CgoFiles"
	CFiles            FileList = "CFiles"
	CxxFiles          FileList = "CXXFiles"
	MFiles            FileList = "MFiles"
	HFiles            FileList = "HFiles"
	FFiles            FileList = "FFiles"
	SFiles            FileList = "SFiles"
	SwigFiles         FileList = "SwigFiles"
	SwigCxxFiles      FileList = "SwigCXXFiles"
	SysoFiles         FileList = "SysoFiles"
	EmbedFiles        FileList = "EmbedFiles"
	IgnoredGoFiles    FileList = "IgnoredGoFiles"
	IgnoredOtherFiles FileList = "IgnoredOtherFiles"
	InvalidGoFiles    FileList = "InvalidGoFiles"
	CompiledGoFiles   FileList = "CompiledGoFiles"
	TestGoFiles       FileList = "TestGoFiles"
	XTestGoFiles      FileList = "XTestGoFiles"
	TestEmbedFiles    FileList = "TestEmbedFiles"
	XTestEmbedFiles   FileList = "XTestEmbedFiles"
)

// BuildFileLists are the lists a go build of the package reads: the files it compiles or links, the
// headers its cgo and assembly include from the package's directory, and the files it embeds. A
// package's test variant names its test files in these lists too, so they are the test binary's when
// the package is. It is a new slice each call, the caller's to extend.
func BuildFileLists() []FileList {
	return []FileList{
		GoFiles,
		CgoFiles,
		CFiles,
		CxxFiles,
		MFiles,
		HFiles,
		FFiles,
		SFiles,
		SwigFiles,
		SwigCxxFiles,
		SysoFiles,
		EmbedFiles,
	}
}

// SourceFiles is the package's file names in lists, list by list in the order lists names them, each
// list as go list printed it: relative to the package's Directory, or absolute in go's cache (Path
// places either). A name in two lists comes twice. Which lists count is the caller's choice, and there
// is no default: the consumers disagree on it (whether ignored files key a build, whether a
// dependency's tests do), and each says its own. No lists is no files.
//
// A FileList that isn't one of this package's constants is a caller's bug, and panics.
func (listed Package) SourceFiles(lists []FileList) []string {
	var names []string
	for _, list := range lists {
		names = append(names, listed.fileList(list)...)
	}
	return names
}

// fileList is the package's names in one list.
func (listed Package) fileList(list FileList) []string {
	switch list {
	case GoFiles:
		return listed.GoFiles
	case CgoFiles:
		return listed.CgoFiles
	case CFiles:
		return listed.CFiles
	case CxxFiles:
		return listed.CxxFiles
	case MFiles:
		return listed.MFiles
	case HFiles:
		return listed.HFiles
	case FFiles:
		return listed.FFiles
	case SFiles:
		return listed.SFiles
	case SwigFiles:
		return listed.SwigFiles
	case SwigCxxFiles:
		return listed.SwigCxxFiles
	case SysoFiles:
		return listed.SysoFiles
	case EmbedFiles:
		return listed.EmbedFiles
	case IgnoredGoFiles:
		return listed.IgnoredGoFiles
	case IgnoredOtherFiles:
		return listed.IgnoredOtherFiles
	case InvalidGoFiles:
		return listed.InvalidGoFiles
	case CompiledGoFiles:
		return listed.CompiledGoFiles
	case TestGoFiles:
		return listed.TestGoFiles
	case XTestGoFiles:
		return listed.XTestGoFiles
	case TestEmbedFiles:
		return listed.TestEmbedFiles
	case XTestEmbedFiles:
		return listed.XTestEmbedFiles
	}
	// Invariant: every FileList a caller holds is one of the constants above, which are the only
	// values this package gives out. Another is a string converted by mistake, and read as an empty
	// list it would leave its files out of whatever the caller keys, unseen.
	panic(fmt.Sprintf("go_listing: %q is no file list go list prints", string(list)))
}

// CgoIncludeDirectories are the directories the package's cgo flags (CgoCFLAGS, CgoCPPFLAGS and
// CgoCXXFLAGS, in that order) name with -I, spelled "-I<directory>" or "-I" then the directory, each
// absolute against the package's Directory and cleaned, each once, in the order the flags name them.
// go list prints `${SRCDIR}` already replaced, and a relative directory already joined to the package's,
// so the joining is for a listing that didn't. A -I with nothing after it names nothing. The headers
// in them are what a cgo package reads from outside its own HFiles, which go's build cache doesn't key.
func (listed Package) CgoIncludeDirectories() []string {
	var directories []string
	for _, flags := range [][]string{listed.CgoCFlags, listed.CgoCppFlags, listed.CgoCxxFlags} {
		for index := 0; index < len(flags); index++ {
			directory, found := strings.CutPrefix(flags[index], "-I")
			if !found {
				continue
			}
			if directory == "" {
				if index+1 == len(flags) {
					continue
				}
				// The directory is the next flag, which is read as the directory and never as a flag.
				index++
				directory = flags[index]
			}
			directory = filepath.Clean(listed.Path(directory))
			if !slices.Contains(directories, directory) {
				directories = append(directories, directory)
			}
		}
	}
	return directories
}
