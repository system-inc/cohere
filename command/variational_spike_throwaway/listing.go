package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// listedPackage is what `go list -json` says of one package in one build, the fields the spike reads.
type listedPackage struct {
	ImportPath      string
	Name            string
	Dir             string
	Standard        bool
	DepOnly         bool
	Export          string
	GoFiles         []string
	CgoFiles        []string
	CompiledGoFiles []string
	Imports         []string
	ImportMap       map[string]string
	Module          *struct {
		Path      string
		GoVersion string
	}
	Error *struct {
		Err string
	}
}

// listedBuild is one build's whole graph as go list gives it, by import path, and the paths the patterns matched.
type listedBuild struct {
	configuration configuration
	cgoEnabled    bool
	packages      map[string]*listedPackage
	matched       []string
	// oursFromExport are the packages of ours that a package not ours imports, directly or through others of ours,
	// which the plain baselines must read from export data as that package's own export data reads them.
	oursFromExport map[string]bool
}

// noFilesError is go list's error for a package whose every file a build's constraints exclude, which is a package
// absent from that build rather than a broken one.
const noFilesError = "build constraints exclude all Go files"

// listBuild runs `go list -e -deps -compiled -json` over patterns in root under build, reading the workspace workFile
// names: the whole graph, every package's compiled files, and nothing compiled. listExports adds the export data.
// depsFromSource makes every package outside the standard library one of ours, as the engine's `--no-cache` run checks
// them from source, the standard library alone restored.
func listBuild(root string, patterns []string, build configuration, workFile string, depsFromSource bool) (*listedBuild, error) {
	environment := build.environment(workFile)
	cgoOn, cgoError := cgoEnabled(root, environment)
	if cgoError != nil {
		return nil, cgoError
	}
	listed := &listedBuild{configuration: build, cgoEnabled: cgoOn, packages: map[string]*listedPackage{}}
	graph, listError := runGoList(root, listed, workFile, []string{"-deps", "-compiled", "-json"}, patterns)
	if listError != nil {
		return nil, listError
	}
	for _, each := range graph {
		// go list names a package's own files from its directory, and cgo's output by its absolute place in the cache;
		// every file is named absolutely here, as go/packages names them for the oracle.
		for index, file := range each.CompiledGoFiles {
			if !filepath.IsAbs(file) {
				each.CompiledGoFiles[index] = filepath.Join(each.Dir, file)
			}
		}
		listed.packages[each.ImportPath] = each
		if !each.DepOnly || depsFromSource && !each.Standard {
			listed.matched = append(listed.matched, each.ImportPath)
		}
	}
	return listed, nil
}

// listExports runs `go list -e -export` naming every package of listed's graph that isn't ours, and those of ours a
// package not ours imports, and gives each its export data file. The rest of ours are checked from source and never
// read from export data, so they are never named and never compiled; go compiles one of ours only when a package not
// ours imports it, and its export data is then named too.
//
// Mutates: listed's packages get their export data files, and listed its oursFromExport.
func listExports(root string, listed *listedBuild, workFile string, ours map[string]*ourPackage) error {
	listed.oursFromExport = map[string]bool{}
	var reached []string
	var names []string
	for path, each := range listed.packages {
		if _, isOurs := ours[path]; isOurs || path == "unsafe" {
			continue
		}
		names = append(names, path)
		for _, imported := range each.Imports {
			if _, isOurs := ours[imported]; isOurs {
				reached = append(reached, imported)
			}
		}
	}
	for len(reached) > 0 {
		path := reached[len(reached)-1]
		reached = reached[:len(reached)-1]
		if listed.oursFromExport[path] || listed.packages[path] == nil {
			continue
		}
		listed.oursFromExport[path] = true
		names = append(names, path)
		for _, imported := range listed.packages[path].Imports {
			if _, isOurs := ours[imported]; isOurs {
				reached = append(reached, imported)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	exported, listError := runGoList(root, listed, workFile, []string{"-export", "-json=ImportPath,Export"}, names)
	if listError != nil {
		return listError
	}
	for _, each := range exported {
		if graphed := listed.packages[each.ImportPath]; graphed != nil {
			graphed.Export = each.Export
		}
	}
	return nil
}

// runGoList runs `go list -e -buildvcs=false` with flags and arguments in root, under listed's build and the workspace
// workFile names, and reads the packages it prints.
func runGoList(
	root string,
	listed *listedBuild,
	workFile string,
	flags []string,
	arguments []string,
) ([]*listedPackage, error) {
	build := listed.configuration
	command := append([]string{"list", "-e", "-buildvcs=false"}, flags...)
	if len(build.tags) != 0 {
		command = append(command, "-tags="+strings.Join(build.tags, ","))
	}
	command = append(command, arguments...)
	goList := exec.Command("go", command...)
	goList.Dir, goList.Env = root, build.environment(workFile)
	var standardError bytes.Buffer
	goList.Stderr = &standardError
	output, runError := goList.Output()
	target := build.target(listed.cgoEnabled)
	if runError != nil {
		return nil, fmt.Errorf("go list %s for %s: %w: %s", strings.Join(flags, " "), target, runError,
			standardError.String())
	}
	var packages []*listedPackage
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		each := &listedPackage{}
		if decodeError := decoder.Decode(each); errors.Is(decodeError, io.EOF) {
			return packages, nil
		} else if decodeError != nil {
			return nil, fmt.Errorf("reading go list for %s: %w", target, decodeError)
		}
		packages = append(packages, each)
	}
}

// exports is the build's export data files by import path, as the merging importer reads them.
func (listed *listedBuild) exports() map[string]string {
	files := make(map[string]string, len(listed.packages))
	for path, each := range listed.packages {
		if each.Export != "" {
			files[path] = each.Export
		}
	}
	return files
}

// sameArchitecture refuses builds that don't share one architecture: the variational check is given one Sizes, and a
// word size that differs by build is a fork the spike's model doesn't hold.
func sameArchitecture(builds []configuration) (string, error) {
	architecture := builds[0].architecture
	for _, build := range builds[1:] {
		if build.architecture != architecture {
			return "", fmt.Errorf("builds mix architectures, %s and %s: the variational check takes one Sizes, so "+
				"every build must share one architecture", architecture, build.architecture)
		}
	}
	return architecture, nil
}
