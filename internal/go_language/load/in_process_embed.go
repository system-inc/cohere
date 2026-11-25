package load

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/module"

	"github.com/system-inc/cohere/internal/listed_directories"
)

// resolveEmbedPatterns is the files a package's `//go:embed` patterns match, relative to its directory,
// slash-separated and sorted, as go list resolves them for EmbedFiles (cmd/go's resolveEmbed): each pattern a glob,
// `all:` keeping the files a directory's walk would skip for starting with a dot or an underscore, no match crossing
// into another module.
// Anything go list would report as the pattern's error is a doubt, returned in place of the files, and so is a
// directory whose name a glob would read as more than a name.
func resolveEmbedPatterns(directory string, patterns []string) ([]string, string) {
	if strings.ContainsAny(directory, `*?[\`) {
		return nil, fmt.Sprintf("the directory %s holds a glob character", directory)
	}
	have := map[string]bool{}
	for _, pattern := range patterns {
		glob, all := strings.CutPrefix(pattern, "all:")
		if _, matchError := path.Match(glob, ""); matchError != nil || glob == "." || !fs.ValidPath(glob) {
			return nil, fmt.Sprintf("the embed pattern %s is not valid", pattern)
		}
		matches, globError := filepath.Glob(directory + string(filepath.Separator) + filepath.FromSlash(glob))
		if globError != nil {
			return nil, fmt.Sprintf("the embed pattern %s: %v", pattern, globError)
		}
		var matched []string
		for _, file := range matches {
			files, doubt := embeddedMatch(directory, file, all)
			if doubt != "" {
				return nil, fmt.Sprintf("the embed pattern %s: %s", pattern, doubt)
			}
			matched = append(matched, files...)
		}
		if len(matched) == 0 {
			return nil, fmt.Sprintf("the embed pattern %s matches no file", pattern)
		}
		for _, file := range matched {
			have[file] = true
		}
	}
	return sortedKeys(have), ""
}

// embeddedMatch is the files one glob match embeds: the file, or every file below the directory, or a doubt.
func embeddedMatch(directory string, file string, all bool) ([]string, string) {
	relative := filepath.ToSlash(strings.TrimPrefix(file, directory+string(filepath.Separator)))
	info, statError := os.Lstat(file)
	if statError != nil {
		return nil, statError.Error()
	}
	for each := file; len(each) > len(directory)+1; each = filepath.Dir(each) {
		if listed_directories.GoModAt(each) {
			return nil, fmt.Sprintf("%s is in another module", relative)
		}
		if each != file {
			if eachInfo, eachError := os.Lstat(each); eachError == nil && !eachInfo.IsDir() {
				return nil, fmt.Sprintf("%s is in something not a directory", relative)
			}
		}
		if badEmbedName(filepath.Base(each)) {
			return nil, fmt.Sprintf("%s has an invalid name", relative)
		}
	}
	switch {
	case info.Mode().IsRegular():
		return []string{relative}, ""
	case info.IsDir():
		return embeddedDirectory(directory, file, all)
	}
	// A symbolic link is embedded only under a `GODEBUG` setting, and the lister answers only with none set.
	return nil, fmt.Sprintf("%s is not a regular file", relative)
}

// embeddedDirectory is every file below a matched directory, as go's walk of it embeds them: stopping at another
// module, skipping a name starting with a dot or an underscore unless all, and a doubt at a name go refuses.
func embeddedDirectory(directory string, matched string, all bool) ([]string, string) {
	var files []string
	var doubt string
	walkError := filepath.WalkDir(matched, func(walked string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		name := entry.Name()
		hidden := name[0] == '.' || name[0] == '_'
		if walked != matched && (badEmbedName(name) || hidden && !all) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			if !hidden {
				doubt = fmt.Sprintf("%s has an invalid name", walked)
				return fs.SkipAll
			}
			return nil
		}
		if entry.IsDir() {
			if listed_directories.GoModAt(walked) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			files = append(files, filepath.ToSlash(strings.TrimPrefix(walked, directory+string(filepath.Separator))))
		}
		return nil
	})
	if walkError != nil {
		return nil, walkError.Error()
	}
	if doubt != "" {
		return nil, doubt
	}
	if len(files) == 0 {
		return nil, fmt.Sprintf("the directory %s holds no file to embed", matched)
	}
	slices.Sort(files)
	return files, ""
}

// badEmbedName reports whether name is one a module cannot hold, or a version control directory's, which go never
// embeds.
func badEmbedName(name string) bool {
	if module.CheckFilePath(name) != nil {
		return true
	}
	return name == "" || name == ".bzr" || name == ".hg" || name == ".git" || name == ".svn"
}
