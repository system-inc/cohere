package build

import (
	"go/build"
	"sync"
)

// PackageReads is the reads of each package directory every listing of one run shares, as it shares Headers: each
// read a listing made itself, kept with the build context it was made under and with which of the directory's files
// were held and their bytes, so a later listing whose build selects every file of the directory as that read's build
// did takes the read rather than matching each file again (#y8cf1wf). Matching every file of every directory was about
// 0.12 CPU-s an extra build's listing at cohere's root on a sealed F64.
//
// A build selects a file by its name's operating system and architecture (goodOSArchFile) and its build constraint
// (shouldBuild), and both ask the build only through matchTag, which records each tag it is asked in the read's
// AllTags before it answers. So a build that answers every tag of a read's AllTags as the read's build answered it
// selects every file of the directory as that build did: each file is asked the same tags, in the same order, and
// answered alike, a file whose name ruled it out never reaching its constraint in either. Of the rest of a Package,
// only what go/build's FindOnly sets depends on the build, the install roots among them, PkgTargetRoot naming
// `GOOS_GOARCH`, and a taken read has those from the taking build's own find (taken); and the `#cgo` lines of a file
// the build selects that imports "C", which go/build matches with no tag recorded (saveCgo), cgo on or off, so such a
// read is never taken.
type PackageReads struct {
	mutex sync.Mutex
	reads map[string][]*packageRead
}

// NewPackageReads is reads no listing has made yet.
func NewPackageReads() *PackageReads {
	return &PackageReads{reads: map[string][]*packageRead{}}
}

// ImportDirectory is the package ImportDirectory reads at directory under context, each Go file's header through
// headers and held, and whether it was taken from a read of directory another listing of the run made rather than read
// here: taken when context answers every tag that read asked as its own build did, every file the read read is held
// as it was then, and no file the read selects imports "C" (see PackageReads). A read made here is kept for the
// listings after it. Nil reads keep and take nothing.
func (packageReads *PackageReads) ImportDirectory(
	context build.Context,
	directory string,
	held func(path string) ([]byte, bool),
	headers *Headers,
) (*build.Package, bool, error) {
	if packageReads == nil {
		built, importError := ImportDirectory(context, directory, held, headers)
		return built, false, importError
	}
	// go/build's own find, under this build, before any read is taken: what it sets is the build's (taken), and an
	// error finding the package is this build's to return, as ImportDirectory returns it.
	found, findError := context.ImportDir(directory, build.FindOnly)
	if findError != nil {
		return found, false, findError
	}
	taking := &Context{Context: context, held: held}
	for _, earlier := range packageReads.readsOf(directory) {
		if earlier.selectsAlike(taking) {
			return earlier.taken(found), true, earlier.importError
		}
	}
	// The files' held bytes are taken before the read, which reads the same bytes: nothing writes a held file while a
	// listing runs (load's Held).
	files := heldFiles(taking, found.Dir)
	built, importsC, importError := importFound(context, found, directory, held, headers)
	packageReads.keep(directory, &packageRead{context: context, built: built, importError: importError,
		importsC: importsC, files: files})
	return built, false, importError
}

// readsOf is the reads of directory kept so far, earliest first. Each is never written once kept, and keep only
// appends past the slice's length, so the caller reads the slice after the lock is released.
func (packageReads *PackageReads) readsOf(directory string) []*packageRead {
	packageReads.mutex.Lock()
	defer packageReads.mutex.Unlock()
	return packageReads.reads[directory]
}

// keep adds read to the reads of directory, for the listings after it. Two listings reading the directory at once
// each keep their own, which is a second read of the same answer, taken by no one but harmless.
func (packageReads *PackageReads) keep(directory string, read *packageRead) {
	packageReads.mutex.Lock()
	defer packageReads.mutex.Unlock()
	packageReads.reads[directory] = append(packageReads.reads[directory], read)
}
