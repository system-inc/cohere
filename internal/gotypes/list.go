// Package gotypes loads Go packages with their syntax and types for the tests that read cohere's own source,
// and never compiles anything to do it (#qnnnkbf).
//
// golang.org/x/tools/go/packages runs `go list -compiled`, `-export` or `-overlay`, each of which compiles,
// and a test runner that runs prebuilt test binaries refuses every go command that compiles. What a test here
// reads comes two ways instead. The packages of the module it inspects are parsed and type-checked from
// source, with any overlay read in place of the files it names, which compiles nothing. Every other package,
// the standard library, the TypeScript checker and its shims, the modules they need, is read from export data
// a product holds (product.go), built once where building is allowed and found by key, or, for a test whose
// point is to type-check everything from source, type-checked from source too.
//
// The only go commands a load runs are `go list -deps -json` and `go env -json`, which read and never build.
// Product.Build alone compiles, with `go list -export`, in a product's TestProduct_ test.
package gotypes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// listedPackage is what `go list -deps -json` says of one package, the fields the loader reads.
type listedPackage struct {
	ImportPath string
	Name       string
	Dir        string
	Standard   bool
	DepOnly    bool
	GoFiles    []string
	CgoFiles   []string
	Imports    []string

	TestGoFiles  []string
	XTestGoFiles []string
	TestImports  []string
	XTestImports []string

	ImportMap  map[string]string
	Deps       []string
	Module     *listedModule
	Error      *struct{ Err string }
	DepsErrors []*struct{ Err string }
}

// listedModule is the module a listed package belongs to.
type listedModule struct {
	Path      string
	Version   string
	Dir       string
	GoMod     string
	GoVersion string
	Replace   *listedModule
}

// listedFields are the fields asked of go list, so it computes nothing else.
var listedFields = "ImportPath,Name,Dir,Standard,DepOnly,GoFiles,CgoFiles,Imports,ImportMap,Deps,Module,Error,DepsErrors," +
	"TestGoFiles,XTestGoFiles,TestImports,XTestImports"

// listing is one `go list -deps -json` run: every package the patterns name and everything they import, in
// the order go prints them, dependencies first.
type listing struct {
	packages []*listedPackage
	byPath   map[string]*listedPackage
}

var listings = struct {
	sync.Mutex
	byRequest map[string]*listingOnce
}{byRequest: map[string]*listingOnce{}}

type listingOnce struct {
	once    sync.Once
	listing *listing
	err     error
}

// list runs go list once per directory, environment and patterns in this process: the tree does not change
// while a test binary runs, and the guards ask for the same listing for each overlay they read.
func list(directory string, environment []string, patterns []string) (*listing, error) {
	request := strings.Join([]string{directory, strings.Join(environment, "\x00"), strings.Join(patterns, "\x00")}, "\x01")
	listings.Lock()
	entry := listings.byRequest[request]
	if entry == nil {
		entry = &listingOnce{}
		listings.byRequest[request] = entry
	}
	listings.Unlock()
	entry.once.Do(func() { entry.listing, entry.err = runList(directory, environment, patterns) })
	return entry.listing, entry.err
}

func runList(directory string, environment []string, patterns []string) (*listing, error) {
	// No "--" before the patterns: a runner's stand-in go refuses any flag it does not know, that one too.
	output, err := goCommand(directory, environment, append([]string{"list", "-e", "-deps", "-json=" + listedFields}, patterns...)...)
	if err != nil {
		return nil, err
	}
	result := &listing{byPath: map[string]*listedPackage{}}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var listed listedPackage
		if err := decoder.Decode(&listed); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading go list's output: %w", err)
		}
		result.packages = append(result.packages, &listed)
		result.byPath[listed.ImportPath] = &listed
	}
	if len(result.packages) == 0 {
		return nil, fmt.Errorf("go list found no packages for %s in %s", strings.Join(patterns, " "), directory)
	}
	return result, nil
}

// goEnvironment is the environment every go command here runs in: this process's, with environment added
// and without any -overlay in GOFLAGS. The loader reads an overlay itself, in place of the files it names,
// and an -overlay go saw would only make go list answer for files the loader reads anyway; a runner that
// never builds refuses a go command whose GOFLAGS carries one.
func goEnvironment(environment []string) []string {
	result := []string{}
	for _, variable := range append(os.Environ(), environment...) {
		if flags, found := strings.CutPrefix(variable, "GOFLAGS="); found {
			kept := []string{}
			for _, flag := range strings.Fields(flags) {
				if !strings.HasPrefix(flag, "-overlay") && !strings.HasPrefix(flag, "--overlay") {
					kept = append(kept, flag)
				}
			}
			variable = "GOFLAGS=" + strings.Join(kept, " ")
		}
		result = append(result, variable)
	}
	return result
}

// goCommand runs one go command in directory and returns its standard output, or an error carrying its
// standard error.
func goCommand(directory string, environment []string, arguments ...string) ([]byte, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	command := exec.Command("go", arguments...)
	command.Dir = absolute
	// PWD names the directory as this process spells it, so go names every file under it the same way and
	// an overlay's paths match go list's, through a symbolic link too (/tmp is /private/tmp on a Mac).
	command.Env = append(goEnvironment(environment), "PWD="+absolute)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, err := command.Output()
	if err != nil {
		shown := strings.Join(arguments, " ")
		if len(shown) > 300 {
			shown = shown[:300] + "..."
		}
		return nil, fmt.Errorf("go %s: %w\n%s", shown, err, strings.TrimSpace(standardError.String()))
	}
	return output, nil
}

// goEnvironmentValues is what go env says of the values that change a package's types, under environment.
func goEnvironmentValues(directory string, environment []string) (map[string]string, error) {
	output, err := goCommand(directory, environment, "env", "-json", "GOVERSION", "GOOS", "GOARCH", "GOEXPERIMENT")
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := json.Unmarshal(output, &values); err != nil {
		return nil, fmt.Errorf("reading go env's output: %w", err)
	}
	if values["GOVERSION"] == "" || values["GOOS"] == "" || values["GOARCH"] == "" {
		return nil, fmt.Errorf("go env named no GOVERSION, GOOS or GOARCH: %v", values)
	}
	return values, nil
}

// moduleAt is the directory of the go.mod at or above directory and the module path it declares.
func moduleAt(directory string) (root string, path string, err error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", "", err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		content, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				if declared, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
					return current, strings.Trim(strings.TrimSpace(declared), `"`), nil
				}
			}
			return "", "", fmt.Errorf("%s names no module", filepath.Join(current, "go.mod"))
		}
		if filepath.Dir(current) == current {
			return "", "", fmt.Errorf("no go.mod at or above %s", absolute)
		}
	}
}

// exportSet is every listed package outside the source module that imports nothing of it, directly or
// not: the packages export data can stand for. A package that imports the source module has to be
// type-checked against the source module's packages as this load reads them, so it is read from source too.
func exportSet(listed *listing, sourceModule string) map[string]bool {
	inSource := map[string]bool{}
	for _, listedPackage := range listed.packages {
		if listedPackage.Module != nil && listedPackage.Module.Path == sourceModule {
			inSource[listedPackage.ImportPath] = true
		}
	}
	exported := map[string]bool{}
	for _, listedPackage := range listed.packages {
		if inSource[listedPackage.ImportPath] {
			continue
		}
		reachesSource := false
		for _, dependency := range listedPackage.Deps {
			reachesSource = reachesSource || inSource[dependency]
		}
		if !reachesSource {
			exported[listedPackage.ImportPath] = true
		}
	}
	return exported
}
