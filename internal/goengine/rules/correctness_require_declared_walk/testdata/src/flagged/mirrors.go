package flagged

import (
	"io/fs"
	"os"
	"path/filepath"
)

// checkMirrors is a helper in a file of its own: the root reaches two walks, one of them a second hop away.
func checkMirrors(root string) []string {
	problems := sourceHeaders(root)
	_ = goFiles(filepath.Join(root, "internal/lint/rules"), nil)
	_ = goFiles(root, func(path string) error { return nil })
	if _, readError := os.ReadFile(filepath.Join(root, "internal/dry/testdata/mirrors.json")); readError != nil {
		problems = append(problems, readError.Error())
	}
	return problems
}

func sourceHeaders(root string) []string {
	var headers []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		headers = append(headers, path)
		return walkError
	})
	return headers
}

func goFiles(root string, visit func(string) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || entry.IsDir() || visit == nil {
			return walkError
		}
		return visit(path)
	})
}
