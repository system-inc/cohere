package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// EnforcedRuleNames is every rule the two tools verify replaces are known to enforce.
//
// It reads `rule-inventory.json`, which is the superset of what eslint and oxlint enforce and the
// denominator the whole project is judged on. Compiled consumers need it because a config's rules
// block is not the whole config: forty rules arrive from `plugins` declarations and are named in no
// block, so a count taken from the block alone understates by exactly the rules nobody wrote down.
//
// That defect appeared three times in one day, in the inventory itself, in the differential harness,
// and in the coverage line, which is the argument for one reader rather than three.
//
// It returns nothing rather than failing when the inventory cannot be read. The caller is a coverage
// note printed alongside a verdict, and refusing to print a run because a supplementary file is
// missing would be worse than printing the run without the note. Callers that need the file to
// exist, like `TestParityAgainstInventory`, read it themselves and fail loudly.
func EnforcedRuleNames() []string {
	path := inventoryPath()
	if path == "" {
		return nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var document struct {
		Rules []struct {
			Rule string `json:"rule"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		return nil
	}

	names := make([]string, 0, len(document.Rules))
	for _, entry := range document.Rules {
		names = append(names, entry.Rule)
	}
	return names
}

// inventoryPath locates the inventory relative to this source file.
//
// Resolved from the compiler's record of where this file was rather than from the working directory
// or the executable's location, because the inventory belongs to the verify source tree and neither
// of those points at it: the binary is installed elsewhere and the working directory is the tree
// under test. It returns empty in a build where that record is unavailable, which the caller treats
// as an absent inventory rather than as an error.
func inventoryPath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "rule-inventory.json")
}
