package load

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// goState is what the lister asks the go command: the build list, the build context go list reads files under, and
// the go.work in use, if any.
type goState struct {
	modules       []*packages.Module
	context       build.Context
	workspaceFile string
}

// internallyLinkedPieTargets are the targets whose default binary is position independent and that the linker can
// still link without an external linker, as the toolchain's internal/platform.InternalLinkPIESupported names them
// among the targets whose binaries are position independent by default (DefaultPIE).
var internallyLinkedPieTargets = []string{"android/arm64", "darwin/amd64", "darwin/arm64", "windows/386",
	"windows/amd64", "windows/arm64"}

// listerFlags are the flags in `GOFLAGS` that change nothing go list prints of a package the lister reads: the
// build tags, which the build context it asks go for holds, and those that only a build or a download reads, the
// parallelism cohere-dev's pool sets among them. `-mod` is among them only as readonly, its default without a vendor
// directory. Any other flag is go list's to answer.
var listerFlags = []string{"tags", "pgo", "trimpath", "buildvcs", "p", "modcacherw"}

// contextTemplate prints the build context go list reads files under, one field a line, every tag list joined by
// commas, which no tag holds.
const contextTemplate = "{{context.GOOS}}\n{{context.GOARCH}}\n{{context.Compiler}}\n{{context.CgoEnabled}}\n" +
	"{{context.GOROOT}}\n{{join context.BuildTags \",\"}}\n{{join context.ToolTags \",\"}}\n" +
	"{{join context.ReleaseTags \",\"}}"

// readGoState runs the two go commands the lister needs, at once, in goEnvironment's workspace under its whole
// environment, reads the go environment from goEnvironment, as go env reports it under that environment, and says why
// it cannot list from them when it cannot. What the commands spend, and what starting them and reading their answers
// costs this process, is added to collector.
//
// go env was the third command here, asked beside the two with `PWD` set to the workspace, and goEnvironment asks
// with the process's own (askGoEnvironment): of what the lister reads, only `GOWORK` reads the working directory, and
// it names the same go.work either way, spelled through a symbolic link or not, which the lister only opens and
// names in a doubt (#4rpt1jh).
func readGoState(goEnvironment *GoEnvironment, collector *timing.Collector) (goState, string) {
	commands := [][]string{
		{"list", "-m", "-json", "all"},
		{"list", "-e", "-f", contextTemplate, "unsafe"},
	}
	outputs := make([][]byte, len(commands))
	failures := make([]error, len(commands))
	// The two run at once, so their children's CPU is read once around both, which a read around each would count
	// twice.
	measureGoCommands(collector, func() {
		var group sync.WaitGroup
		for index, arguments := range commands {
			group.Go(func() {
				measureListing(collector, func() {
					outputs[index], failures[index] = goOutput(goEnvironment.workspace, goEnvironment.fullEnvironment,
						arguments)
				})
			})
		}
		group.Wait()
	})
	for index, failure := range failures {
		if failure != nil {
			return goState{}, fmt.Sprintf("go %s: %v", strings.Join(commands[index], " "), failure)
		}
	}

	if goVersion := goEnvironment.value("GOVERSION"); goVersion != runtime.Version() {
		// go/build is the engine's, and go list reads files with the toolchain's own copy of it.
		return goState{}, fmt.Sprintf("the go command is %s and the engine was built with %s", goVersion,
			runtime.Version())
	}
	if goEnvironment.value("GODEBUG") != "" {
		return goState{}, "GODEBUG is set, and go list reads some of its settings"
	}
	if fips140 := goEnvironment.value("GOFIPS140"); fips140 != "" && fips140 != "off" {
		// A `GOFIPS140` snapshot of the cryptographic module moves the standard library's crypto/internal/fips140
		// imports to a versioned copy.
		return goState{}, fmt.Sprintf("GOFIPS140 is %s", fips140)
	}
	if doubt := listerFlagsOnly(goEnvironment.value("GOFLAGS")); doubt != "" {
		return goState{}, doubt
	}

	context, doubt := readContext(outputs[1])
	if doubt != "" {
		return goState{}, doubt
	}
	modules, doubt := readModules(outputs[0])
	if doubt != "" {
		return goState{}, doubt
	}
	state := goState{modules: modules, context: context}
	if workspaceFile := goEnvironment.value("GOWORK"); workspaceFile != "off" {
		state.workspaceFile = workspaceFile
	}
	return state, ""
}

// goOutput runs go with arguments in workspace, as list runs go list, and returns what it printed.
func goOutput(workspace string, environment []string, arguments []string) ([]byte, error) {
	command := exec.Command("go", arguments...)
	command.Dir = workspace
	// PWD as in runList, so the directories go prints are spelled as the load spells the workspace.
	command.Env = append(slices.Clone(environment), "PWD="+workspace)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("%w: %s", runError, strings.TrimSpace(standardError.String()))
	}
	if said := strings.TrimSpace(standardError.String()); said != "" {
		// go list -e says what is wrong on standard error and still exits cleanly, and anything it says is a doubt.
		return nil, errors.New(said)
	}
	return output, nil
}

// listerFlagsOnly is a doubt when `GOFLAGS` holds a flag beyond listerFlags, or a `-mod` other than readonly.
func listerFlagsOnly(goFlags string) string {
	for _, flag := range strings.Fields(goFlags) {
		name, value, _ := strings.Cut(strings.TrimLeft(flag, "-"), "=")
		if slices.Contains(listerFlags, name) || name == "mod" && value == "readonly" {
			continue
		}
		return fmt.Sprintf("GOFLAGS holds %s", flag)
	}
	return ""
}

// readContext is the build context contextTemplate printed, on a target the lister answers for.
func readContext(output []byte) (build.Context, string) {
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) != 8 {
		return build.Context{}, fmt.Sprintf("go list printed a build context of %d lines", len(lines))
	}
	context := build.Default
	context.GOOS = lines[0]
	context.GOARCH = lines[1]
	context.Compiler = lines[2]
	context.CgoEnabled = lines[3] == "true"
	context.GOROOT = lines[4]
	context.GOPATH = ""
	context.Dir = ""
	context.BuildTags = tagList(lines[5])
	context.ToolTags = tagList(lines[6])
	context.ReleaseTags = tagList(lines[7])
	if context.Compiler != "gc" {
		return build.Context{}, fmt.Sprintf("the compiler is %s", context.Compiler)
	}
	if linksExternally(context.GOOS, context.GOARCH) {
		// The linker needs runtime/cgo there, and with cgo off go list reports every command as an error.
		return build.Context{}, fmt.Sprintf("every command for %s/%s links externally", context.GOOS,
			context.GOARCH)
	}
	if context.GOROOT == "" {
		return build.Context{}, "go list printed no GOROOT"
	}
	return context, ""
}

// linksExternally reports whether every command for the target is linked by an external linker, whatever the flags,
// as the go command's externalLinkingReason decides it for a build whose flags name no build mode, no linking mode
// and no shared linking, the only builds the lister answers (see listerFlags): the targets internal/platform's
// MustLinkExternal names for a program without cgo, and those whose binaries are position independent by default and
// cannot be linked so internally.
func linksExternally(operatingSystem string, architecture string) bool {
	switch operatingSystem {
	case "android":
		if architecture != "arm64" {
			return true
		}
	case "ios":
		if architecture == "arm64" {
			return true
		}
	}
	defaultPie := slices.Contains([]string{"android", "ios", "windows", "darwin"}, operatingSystem)
	return defaultPie && !slices.Contains(internallyLinkedPieTargets, operatingSystem+"/"+architecture)
}

// linkerDependencies are the imports go's linker adds to every command on the target, which go list walks from a
// package main as it walks its imports (the go command's LinkerDeps): runtime everywhere, and math on arm for its
// soft floating point. The rest it adds come from flags the lister does not carry (see listerFlags), or from external
// linking, where readContext doubts.
func linkerDependencies(architecture string) []string {
	if architecture == "arm" {
		return []string{"runtime", "math"}
	}
	return []string{"runtime"}
}

// tagList is the tags a comma-joined line names.
func tagList(line string) []string {
	if line == "" {
		return nil
	}
	return strings.Split(line, ",")
}

// readModules is the build list `go list -m -json all` printed.
func readModules(output []byte) ([]*packages.Module, string) {
	var modules []*packages.Module
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		each := &packages.Module{}
		decodeError := decoder.Decode(each)
		if errors.Is(decodeError, io.EOF) {
			return modules, ""
		}
		if decodeError != nil {
			return nil, fmt.Sprintf("reading go list -m's output: %v", decodeError)
		}
		modules = append(modules, each)
	}
}

// readMainModules reads each main module's go.mod and the go.work in use, for what go list would read in them that
// the lister does not carry, and collects every requirement they name.
func (lister *inProcessLister) readMainModules(workspaceFile string) string {
	for _, main := range lister.mainModules {
		if slices.Contains(strings.Split(main.Path, "/"), "vendor") {
			// go's patterns treat a vendor element of a path as a vendored package's.
			return fmt.Sprintf("the main module %s has a vendor element in its path", main.Path)
		}
		if !standardImportPathFree(main.Path) {
			return fmt.Sprintf("the main module %s has a path the standard library could hold", main.Path)
		}
		if main.GoMod == "" || main.Dir == "" {
			return fmt.Sprintf("the main module %s has no go.mod", main.Path)
		}
		contents, readError := os.ReadFile(main.GoMod)
		if readError != nil {
			return fmt.Sprintf("reading %s: %v", main.GoMod, readError)
		}
		parsed, parseError := modfile.Parse(main.GoMod, contents, nil)
		if parseError != nil {
			return fmt.Sprintf("parsing %s: %v", main.GoMod, parseError)
		}
		if parsed.Go == nil || version.Compare("go"+parsed.Go.Version, "go1.17") < 0 {
			// Before go 1.17 a module's graph is not pruned, and the lister checks imports as a pruned graph has them.
			return fmt.Sprintf("%s is for a go before 1.17", main.GoMod)
		}
		if len(parsed.Ignore) > 0 || len(parsed.Godebug) > 0 {
			return fmt.Sprintf("%s has an ignore or a godebug directive", main.GoMod)
		}
		if exists(filepath.Join(main.Dir, "vendor")) {
			return fmt.Sprintf("%s has a vendor directory", main.Dir)
		}
		for _, required := range parsed.Require {
			lister.required[required.Mod.Path+"@"+required.Mod.Version] = true
		}
	}
	if workspaceFile == "" {
		return ""
	}
	contents, readError := os.ReadFile(workspaceFile)
	if readError != nil {
		return fmt.Sprintf("reading %s: %v", workspaceFile, readError)
	}
	parsed, parseError := modfile.ParseWork(workspaceFile, contents, nil)
	if parseError != nil {
		return fmt.Sprintf("parsing %s: %v", workspaceFile, parseError)
	}
	if len(parsed.Godebug) > 0 {
		return fmt.Sprintf("%s has a godebug directive", workspaceFile)
	}
	if exists(filepath.Join(filepath.Dir(workspaceFile), "vendor")) {
		return fmt.Sprintf("%s has a vendor directory beside it", workspaceFile)
	}
	return ""
}

// standardImportPathFree reports whether no package of the module at modulePath could be taken for a standard one,
// as one whose path's first element has no dot could.
func standardImportPathFree(modulePath string) bool {
	return !standardImportPath(modulePath)
}
