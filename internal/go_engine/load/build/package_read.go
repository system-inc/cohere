package build

import (
	"bytes"
	"go/build"
)

// packageRead is one listing's read of a directory: the build context it was read under, go/build's Package and
// error, whether a file it selects imports "C", and each of the directory's files as the read read it, held or not and
// which bytes.
type packageRead struct {
	context     build.Context
	built       *build.Package
	importError error
	importsC    bool
	files       []heldFile
}

// heldFile is one file of a read directory, by its path, and whether the run held it and which bytes when the
// directory was read.
type heldFile struct {
	path   string
	held   bool
	source []byte
}

// selectsAlike reports whether the build of taking selects every file of the read's directory as the read's build
// did, and reads the same bytes of each, so the read is taking's own but for what its find sets (see PackageReads).
func (read *packageRead) selectsAlike(taking *Context) bool {
	if read.built == nil || read.importsC {
		return false
	}
	if !sameBeyondTags(read.context, taking.Context) {
		return false
	}
	reading := &Context{Context: read.context}
	for _, tag := range read.built.AllTags {
		// Without allTags, which the read already holds: matchTag answers alike with it or without.
		if reading.matchTag(tag, nil) != taking.matchTag(tag, nil) {
			return false
		}
	}
	for _, file := range read.files {
		var source []byte
		held := false
		if taking.held != nil {
			source, held = taking.held(file.path)
		}
		// bytes.Equal answers at once for the same bytes, by their address, as the run holds one slice for a file
		// until it writes the file (Headers.header).
		if held != file.held || held && !bytes.Equal(source, file.source) {
			return false
		}
	}
	return true
}

// sameBeyondTags reports whether two build contexts differ in nothing a Package reads but what matchTag answers from:
// the operating system, the architecture, the build, tool and release tags, and cgo. Their functions are not
// compared: every listing that shares PackageReads reads through the one run's (load's SharedReads), and opens a held
// file from the bytes held gives, which selectsAlike compares.
func sameBeyondTags(read build.Context, taking build.Context) bool {
	return read.GOROOT == taking.GOROOT && read.GOPATH == taking.GOPATH && read.Dir == taking.Dir &&
		read.UseAllFiles == taking.UseAllFiles && read.Compiler == taking.Compiler &&
		read.InstallSuffix == taking.InstallSuffix
}

// taken is the read as the build that found its package as found takes it: each field go/build's Import sets before
// it returns for FindOnly is found's, the build's own, and every other field is the read's. The Package is a copy,
// its lists the read's, shared: every reader only reads them, and the lister's readFrom copies Imports before it
// writes.
func (read *packageRead) taken(found *build.Package) *build.Package {
	taken := *read.built
	taken.Dir = found.Dir
	taken.ImportPath = found.ImportPath
	taken.Root = found.Root
	taken.SrcRoot = found.SrcRoot
	taken.PkgRoot = found.PkgRoot
	taken.PkgTargetRoot = found.PkgTargetRoot
	taken.BinDir = found.BinDir
	taken.Goroot = found.Goroot
	taken.PkgObj = found.PkgObj
	taken.ConflictDir = found.ConflictDir
	return &taken
}

// heldFiles is every file of directory, by the path the copied Import opens it at, and whether reading held it and
// which bytes: a directory's entries hold for the run (load's SharedReads), so a later build reads the same files, and
// the bytes say whether it reads them as this read does. A directory that cannot be read has none, and its read fails
// alike in every build.
func heldFiles(reading *Context, directory string) []heldFile {
	entries, readError := reading.readDir(directory)
	if readError != nil {
		return nil
	}
	files := make([]heldFile, 0, len(entries))
	for _, entry := range entries {
		// A directory is no file the read reads; a link to one is kept as a file, which only compares more.
		if entry.IsDir() {
			continue
		}
		file := heldFile{path: reading.joinPath(directory, entry.Name())}
		if reading.held != nil {
			file.source, file.held = reading.held(file.path)
		}
		files = append(files, file)
	}
	return files
}
