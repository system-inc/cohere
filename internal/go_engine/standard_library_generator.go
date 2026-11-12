package go_engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The hidden verbs a generator starts its children with, after its own prefix (see standardLibraryGenerator).
const (
	// standardLibraryVariantVerb runs the engine over one variant's probe and writes the table of what it collected.
	standardLibraryVariantVerb = "standard-library-variant"

	// standardLibraryCanonicalVerb writes the variants' tables again as one, in a fresh process (see
	// check.CanonicalStandardLibraryTable).
	standardLibraryCanonicalVerb = "standard-library-canonical"
)

// probeFileName is the one Go file of a variant's probe module, whose imports the child reads back to check that it
// collected every package the probe asked for.
const probeFileName = "probe.go"

// standardLibraryGenerator computes the standard library's entries for one variant at a time, each in a child process
// of its own over a probe module that imports every importable package, and writes them as one table in a last child.
// The release's generator and its check both use it, so the check recomputes a variant exactly as it was generated.
type standardLibraryGenerator struct {
	// executable and prefix start a child: this binary and the go-engine verb, before the child's verb and its own
	// arguments, or a test binary whose TestMain runs the child.
	executable string
	prefix     []string

	// stamp is the release stamp every key holds, the binary's own. An empty stamp generates nothing.
	stamp string

	// environment is added after each variant's own to every command the generator starts, nothing in a release; a
	// test sets `GOPROXY=off` there, so a toolchain it names but does not have fails without the network.
	environment []string

	// packages, when not empty, are what the probe imports in place of every importable package go list names, so a
	// test can generate a small table: the whole of the standard library is too slow for one.
	packages []string
}

// standardLibraryVariantRequest is one variant a generator is asked for: a Go version as `GOTOOLCHAIN` names it, a
// target as `GOOS/GOARCH`, and a `CGO_ENABLED` value.
type standardLibraryVariantRequest struct {
	goVersion  string
	target     string
	cgoEnabled string
}

// variantLabel is a variant as the generator's lines and errors name it.
func variantLabel(request standardLibraryVariantRequest) string {
	return request.goVersion + " " + request.target + " cgo " + request.cgoEnabled
}

// variantEnvironment is what every command for request runs under, beside the process's own: the toolchain, the
// target and cgo set explicitly, never inherited, since a cross target's go env reports `CGO_ENABLED=0` and a native
// one with a C compiler 1; `GOFLAGS` and `GOEXPERIMENT` empty, as a user who set neither reads them, whatever the
// machine's go env file says; and no go.work, so nothing above the probe joins its build.
func (generator standardLibraryGenerator) variantEnvironment(request standardLibraryVariantRequest) []string {
	goos, goarch, _ := strings.Cut(request.target, "/")
	environment := []string{
		"GOTOOLCHAIN=" + request.goVersion,
		"GOOS=" + goos,
		"GOARCH=" + goarch,
		"CGO_ENABLED=" + request.cgoEnabled,
		"GOFLAGS=",
		"GOEXPERIMENT=",
		"GOWORK=off",
	}
	return append(environment, generator.environment...)
}

// variantTable is the table of request's standard library, as its child wrote it with StandardLibraryTableOf, and the
// variant it says it holds. The toolchain is fetched first, by asking it for go env under `GOTOOLCHAIN`, and a failed
// fetch is an error naming the version, as is a toolchain that reports another version, target or `CGO_ENABLED`
// than was asked, a child that fails, and a table that says it holds another variant: a release never ships a
// variant it did not compute as asked.
func (generator standardLibraryGenerator) variantTable(
	request standardLibraryVariantRequest,
) ([]byte, cache.StandardLibraryVariant, error) {
	if generator.stamp == "" {
		return nil, cache.StandardLibraryVariant{}, errors.New("generating the standard library's entries: this " +
			"binary carries no release stamp, and every key holds it")
	}
	directory, directoryError := os.MkdirTemp("", "cohere-standard-library-")
	if directoryError != nil {
		return nil, cache.StandardLibraryVariant{}, fmt.Errorf("generating %s: %w", variantLabel(request),
			directoryError)
	}
	// Ignored: the directory is a temporary one of this call's own, and a failure to remove it changes no table.
	defer func() { _ = os.RemoveAll(directory) }()
	environment := generator.variantEnvironment(request)
	goEnvironment, environmentError := load.ReadGoEnvironment(directory, environment, nil)
	if environmentError != nil {
		return nil, cache.StandardLibraryVariant{}, fmt.Errorf("fetching the toolchain %s with GOTOOLCHAIN: %w",
			request.goVersion, environmentError)
	}
	values := goEnvironment.Values()
	if reported := (standardLibraryVariantRequest{goVersion: values["GOVERSION"], target: values["GOOS"] + "/" +
		values["GOARCH"], cgoEnabled: values["CGO_ENABLED"]}); reported != request {
		return nil, cache.StandardLibraryVariant{}, fmt.Errorf("generating %s: go env reports %s instead",
			variantLabel(request), variantLabel(reported))
	}
	packages := generator.packages
	if len(packages) == 0 {
		listed, listError := importableStandardLibrary(directory, environment)
		if listError != nil {
			return nil, cache.StandardLibraryVariant{}, fmt.Errorf("generating %s: %w", variantLabel(request),
				listError)
		}
		packages = listed
	}
	probe := filepath.Join(directory, "probe")
	if probeError := writeProbe(probe, request.goVersion, packages); probeError != nil {
		return nil, cache.StandardLibraryVariant{}, fmt.Errorf("generating %s: %w", variantLabel(request), probeError)
	}
	output := filepath.Join(directory, "variant.table")
	if childError := generator.runChild(environment, standardLibraryVariantVerb, "--root", probe, "--out",
		output); childError != nil {
		return nil, cache.StandardLibraryVariant{}, fmt.Errorf("generating %s: %w", variantLabel(request), childError)
	}
	table, readError := os.ReadFile(output)
	if readError != nil {
		return nil, cache.StandardLibraryVariant{}, fmt.Errorf("generating %s: reading the child's table: %w",
			variantLabel(request), readError)
	}
	variants := cache.NewStandardLibraryTable(table).Variants()
	if len(variants) != 1 || variants[0].Target != request.target || variants[0].CgoEnabled != request.cgoEnabled ||
		variants[0].GoVersion != request.goVersion {
		return nil, cache.StandardLibraryVariant{}, fmt.Errorf("generating %s: the child's table holds %+v",
			variantLabel(request), variants)
	}
	return table, variants[0], nil
}

// canonicalTable is the one table the variants' tables are written as, by a child that has encoded nothing before
// (see check.CanonicalStandardLibraryTable), so the same variants give the same bytes on every run.
func (generator standardLibraryGenerator) canonicalTable(tables [][]byte) ([]byte, error) {
	directory, directoryError := os.MkdirTemp("", "cohere-standard-library-")
	if directoryError != nil {
		return nil, fmt.Errorf("writing the canonical table: %w", directoryError)
	}
	// Ignored: the directory is a temporary one of this call's own, and a failure to remove it changes no table.
	defer func() { _ = os.RemoveAll(directory) }()
	output := filepath.Join(directory, "canonical.table")
	arguments := []string{"--out", output}
	for index, table := range tables {
		input := filepath.Join(directory, "variant-"+strconv.Itoa(index)+".table")
		if writeError := os.WriteFile(input, table, 0o644); writeError != nil {
			return nil, fmt.Errorf("writing the canonical table: %w", writeError)
		}
		arguments = append(arguments, input)
	}
	if childError := generator.runChild(generator.environment, standardLibraryCanonicalVerb,
		arguments...); childError != nil {
		return nil, fmt.Errorf("writing the canonical table: %w", childError)
	}
	table, readError := os.ReadFile(output)
	if readError != nil {
		return nil, fmt.Errorf("writing the canonical table: reading the child's table: %w", readError)
	}
	return table, nil
}

// runChild runs verb in a child of the generator under environment, beside the process's own, and is an error when
// the child exits other than 0, holding what it said on stderr.
func (generator standardLibraryGenerator) runChild(environment []string, verb string, arguments ...string) error {
	command := exec.Command(generator.executable, append(append(slices.Clone(generator.prefix), verb),
		arguments...)...)
	command.Env = append(os.Environ(), environment...)
	var standardError bytes.Buffer
	command.Stdout = &standardError
	command.Stderr = &standardError
	if runError := command.Run(); runError != nil {
		return fmt.Errorf("running %s in a child: %w: %s", verb, runError, strings.TrimSpace(standardError.String()))
	}
	return nil
}

// importableStandardLibrary is every package go list names in std under environment that a module outside the
// standard library can import, sorted: all but an internal package, which only its own tree may import, and a vendored
// one, which only the standard library resolves. Both are still reached, as dependencies of the packages that import
// them. A package go list cannot list for the target is an error, rather than a package the table quietly leaves out.
func importableStandardLibrary(directory string, environment []string) ([]string, error) {
	command := exec.Command("go", "list", "-e", "-json=ImportPath,Error", "std")
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("listing the standard library with go list: %w: %s", runError,
			strings.TrimSpace(standardError.String()))
	}
	var importable []string
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var listed struct {
			ImportPath string
			Error      *struct {
				Message string `json:"Err"`
			}
		}
		if decodeError := decoder.Decode(&listed); decodeError != nil {
			return nil, fmt.Errorf("reading go list's answer: %w", decodeError)
		}
		if listed.Error != nil {
			return nil, fmt.Errorf("listing the standard library with go list: %s: %s", listed.ImportPath,
				listed.Error.Message)
		}
		elements := strings.Split(listed.ImportPath, "/")
		if elements[0] == "vendor" || slices.Contains(elements, "internal") {
			continue
		}
		importable = append(importable, listed.ImportPath)
	}
	slices.Sort(importable)
	return importable, nil
}

// writeProbe writes, at directory, a module whose one package imports every one of packages, so a run over it
// reaches each of them and everything they import. Its go line is goVersion's language version, which the toolchain
// `GOTOOLCHAIN` names always satisfies.
func writeProbe(directory string, goVersion string, packages []string) error {
	if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
		return fmt.Errorf("writing the probe: %w", makeError)
	}
	language := strings.TrimPrefix(version.Lang(goVersion), "go")
	if language == "" {
		return fmt.Errorf("writing the probe: %q is no Go version", goVersion)
	}
	module := "module cohere.invalid/standardlibraryprobe\n\ngo " + language + "\n"
	var source strings.Builder
	source.WriteString("// Package probe imports every standard-library package the release computes, so one run " +
		"reaches all of them.\npackage probe\n\nimport (\n")
	for _, imported := range slices.Sorted(slices.Values(packages)) {
		source.WriteString("\t_ " + strconv.Quote(imported) + "\n")
	}
	source.WriteString(")\n")
	for name, contents := range map[string]string{"go.mod": module, probeFileName: source.String()} {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			return fmt.Errorf("writing the probe: %w", writeError)
		}
	}
	return nil
}
