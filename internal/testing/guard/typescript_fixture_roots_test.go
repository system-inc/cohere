package guard

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/repository"
	"github.com/system-inc/cohere/internal/testing/typescript_fixtures"
)

// The format proofs that the fix pass's guard's tree is the tree the formatter would parse read every TypeScript
// fixture by the directories typescript_fixtures names, so Go's test cache keys estree and native on those files
// alone, never the whole module (#0zdyk51). That list is only as good as its coverage, so this walks the whole
// module, as the other checks here do, and every TypeScript file must be under a named root. A fixture added
// anywhere else fails here by name instead of quietly leaving the proofs' corpus.
func TestEveryTypeScriptFixtureIsUnderANamedRoot(test *testing.T) {
	test.Parallel()
	root, rootError := repository.Root()
	if rootError != nil {
		test.Fatal(rootError)
	}
	named := make([]string, len(typescript_fixtures.Roots))
	for index, relative := range typescript_fixtures.Roots {
		named[index] = filepath.Join(root, filepath.FromSlash(relative)) + string(filepath.Separator)
	}
	files := typescript_fixtures.Under(root, ".ts", ".tsx", ".mts", ".cts")
	if len(files) < 50 {
		test.Fatalf("the module holds %d TypeScript files, too few for this walk to have read it", len(files))
	}
	for _, file := range files {
		covered := false
		for _, prefix := range named {
			if strings.HasPrefix(file, prefix) {
				covered = true
				break
			}
		}
		if !covered {
			relative, relativePathError := filepath.Rel(root, file)
			if relativePathError != nil {
				relative = file
			}
			test.Errorf(
				"%s is a TypeScript file under no root typescript_fixtures.Roots names, so the format proofs "+
					"never read it: add the testdata directory that holds it to Roots",
				filepath.ToSlash(relative),
			)
		}
	}
}
