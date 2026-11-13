package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// standardLibraryTableSource is the release's file that lists the Go versions its standard library tables hold, and
// standardLibraryTableVersionsName the constant that lists them, read from each side's own source, so a side whose
// release lists other versions is checked for its own.
const (
	standardLibraryTableSource       = "internal/release/packaging/standard_library_table.go"
	standardLibraryTableVersionsName = "standardLibraryTableGoVersions"
)

// toolchainModule is the module go fetches a toolchain as, for a version as `GOTOOLCHAIN` names it, on a platform.
func toolchainModule(goVersion string, goOperatingSystem string, goArchitecture string) string {
	return "golang.org/toolchain@v0.0.1-" + goVersion + "." + goOperatingSystem + "-" + goArchitecture
}

// runGo runs the go at goBinary in directory, the working directory when it is empty, with environment, and returns
// what it printed, trimmed, carrying its stderr into the error.
func runGo(goBinary string, directory string, environment []string, arguments ...string) (string, error) {
	command := exec.Command(goBinary, arguments...)
	command.Dir = directory
	command.Env = environment
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return "", fmt.Errorf("go %s: %w: %s", strings.Join(arguments, " "), runError,
			strings.TrimSpace(standardError.String()))
	}
	return strings.TrimSpace(string(output)), nil
}

// standardLibraryTableVersions are the Go versions a side's release generates its standard library tables under, read
// from the side's source by parsing the constant that lists them, as `GOTOOLCHAIN` names each.
func standardLibraryTableVersions(source string) ([]string, error) {
	path := filepath.Join(source, filepath.FromSlash(standardLibraryTableSource))
	parsed, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if parseError != nil {
		return nil, fmt.Errorf("reading the standard library table's versions: %w", parseError)
	}
	for _, declaration := range parsed.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue || len(value.Names) != 1 || value.Names[0].Name != standardLibraryTableVersionsName ||
				len(value.Values) != 1 {
				continue
			}
			literal, isLiteral := value.Values[0].(*ast.BasicLit)
			if !isLiteral || literal.Kind != token.STRING {
				break
			}
			listed, unquoteError := strconv.Unquote(literal.Value)
			if unquoteError != nil {
				return nil, fmt.Errorf("reading %s in %s: %w", standardLibraryTableVersionsName, path, unquoteError)
			}
			return strings.Split(listed, ","), nil
		}
	}
	return nil, fmt.Errorf("%s holds no %s string, so its release builds no standard library table the pair could "+
		"check is offline", path, standardLibraryTableVersionsName)
}

// requiredGoVersion is the go a module's go.mod asks for: its toolchain line, or its go line when it has none.
func requiredGoVersion(directory string) (string, error) {
	path := filepath.Join(directory, "go.mod")
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return "", readError
	}
	parsed, parseError := modfile.Parse(path, contents, nil)
	if parseError != nil {
		return "", parseError
	}
	if parsed.Toolchain != nil {
		return parsed.Toolchain.Name, nil
	}
	if parsed.Go != nil {
		return "go" + parsed.Go.Version, nil
	}
	return "", fmt.Errorf("%s names no go version", path)
}

// downloadModules runs `go mod download -json` in directory with arguments, and returns every module it reports, each
// with the error it met, if any. Without arguments, in a workspace or a module, it takes the whole build list, every
// module any build of the tree can read and more, so a pass proves the engine's loads and the release's builds read
// nothing from the network; it is the cheapest command that does, since it reads the graph and compiles nothing. It
// fails when go does, with the reported modules still returned, so a caller names each.
func downloadModules(
	goBinary string,
	directory string,
	environment []string,
	arguments ...string,
) ([]downloadedModule, error) {
	command := exec.Command(goBinary, append([]string{"mod", "download", "-json"}, arguments...)...)
	command.Dir = directory
	command.Env = environment
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	modules := []downloadedModule{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		module := downloadedModule{}
		decodeError := decoder.Decode(&module)
		if errors.Is(decodeError, io.EOF) {
			break
		}
		if decodeError != nil {
			return modules, fmt.Errorf("go mod download -json in %s printed what is not JSON: %w", directory,
				decodeError)
		}
		modules = append(modules, module)
	}
	if runError != nil {
		return modules, fmt.Errorf("go mod download in %s: %w: %s", directory, runError,
			strings.TrimSpace(standardError.String()))
	}
	return modules, nil
}

// checkOffline proves the machine holds what measuring needs with the network off, and refuses naming each missing
// piece: each side's go, as its go.mod asks for it; every toolchain each side's release generates its standard library
// tables under, by `GOTOOLCHAIN=<version> go version`; and the module graph of each side and each corpus, by
// downloadModules. environment is offlineGoEnvironment's, and toolchains are tried in neutral, which holds no go.mod.
func checkOffline(
	goBinary string,
	environment []string,
	neutral string,
	sides []goDirectory,
	corpora []goDirectory,
	say progress,
) error {
	missing := []string{}
	versions := []string{}
	for _, side := range sides {
		required, requiredError := requiredGoVersion(side.path)
		selected, selectedError := runGo(goBinary, side.path, environment, "env", "GOVERSION")
		switch {
		case requiredError != nil:
			missing = append(missing, fmt.Sprintf("%s's go.mod: %v", side.label, requiredError))
		case selectedError != nil:
			missing = append(missing, fmt.Sprintf("%s asks for %s, which go cannot have offline: %v", side.label,
				required, selectedError))
		case version.Compare(selected, required) < 0:
			missing = append(missing, fmt.Sprintf("%s asks for %s and go runs %s", side.label, required, selected))
		default:
			say.say("offline: %s asks for %s and builds with %s\n", side.label, required, selected)
		}
		listed, versionsError := standardLibraryTableVersions(side.path)
		if versionsError != nil {
			missing = append(missing, fmt.Sprintf("%s: %v", side.label, versionsError))
		}
		for _, listedVersion := range listed {
			if !slices.Contains(versions, listedVersion) {
				versions = append(versions, listedVersion)
			}
		}
	}
	for _, toolchain := range versions {
		toolchainEnvironment := append(environmentWithout(environment, "GOTOOLCHAIN"), "GOTOOLCHAIN="+toolchain)
		if _, toolchainError := runGo(goBinary, neutral, toolchainEnvironment, "version"); toolchainError != nil {
			missing = append(missing, fmt.Sprintf("toolchain %s, which the standard library tables are generated "+
				"under, is not in the module cache: %v", toolchain, toolchainError))
			continue
		}
		say.say("offline: toolchain %s\n", toolchain)
	}
	for _, directory := range slices.Concat(sides, corpora) {
		modules, downloadError := downloadModules(goBinary, directory.path, environment)
		unavailable := 0
		for _, module := range modules {
			if module.Error != "" {
				unavailable++
				missing = append(missing, fmt.Sprintf("%s needs module %s@%s, not in the module cache: %s",
					directory.label, module.Path, module.Version, module.Error))
			}
		}
		if downloadError != nil && unavailable == 0 {
			missing = append(missing, fmt.Sprintf("%s's module graph: %v", directory.label, downloadError))
		}
		if downloadError == nil {
			say.say("offline: %s's %d modules\n", directory.label, len(modules))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the machine cannot measure with the network off; run --prepare where it is on, into the "+
			"module cache this run reads. Missing:\n  %s", strings.Join(missing, "\n  "))
	}
	return nil
}

// prepareModules fills the module cache, with the network on, with what checkOffline checks: each side's toolchain,
// every toolchain the sides' releases generate their tables under, as modules for this platform, and every module of
// each side's and each corpus's graph. It returns them as "path version" lines, sorted, for an image to be baked from.
func prepareModules(
	goBinary string,
	environment []string,
	neutral string,
	sides []goDirectory,
	corpora []goDirectory,
	goOperatingSystem string,
	goArchitecture string,
) ([]string, error) {
	toolchains := []string{}
	for _, directory := range slices.Concat(sides, corpora) {
		// go fetches a toolchain the directory's go.mod asks for as it starts here, which is what puts it in the cache.
		selected, selectedError := runGo(goBinary, directory.path, environment, "env", "GOVERSION")
		if selectedError != nil {
			return nil, fmt.Errorf("%s: %w", directory.label, selectedError)
		}
		toolchains = append(toolchains, toolchainModule(selected, goOperatingSystem, goArchitecture))
	}
	for _, side := range sides {
		listed, versionsError := standardLibraryTableVersions(side.path)
		if versionsError != nil {
			return nil, fmt.Errorf("%s: %w", side.label, versionsError)
		}
		for _, listedVersion := range listed {
			toolchains = append(toolchains, toolchainModule(listedVersion, goOperatingSystem, goArchitecture))
		}
	}
	slices.Sort(toolchains)
	toolchains = slices.Compact(toolchains)

	lines := []string{}
	prepared, toolchainsError := downloadModules(goBinary, neutral, environment, toolchains...)
	if toolchainsError != nil {
		return nil, fmt.Errorf("the toolchains: %w", toolchainsError)
	}
	for _, directory := range slices.Concat(sides, corpora) {
		modules, downloadError := downloadModules(goBinary, directory.path, environment)
		if downloadError != nil {
			return nil, fmt.Errorf("%s: %w", directory.label, downloadError)
		}
		prepared = append(prepared, modules...)
	}
	for _, module := range prepared {
		lines = append(lines, module.Path+" "+module.Version)
	}
	slices.Sort(lines)
	return slices.Compact(lines), nil
}
