package unused_code

import (
	"fmt"

	"github.com/system-inc/go-types/types"
)

// unsafeImporter imports package unsafe and no other, the one import a record test's shapes need, so no shape reads
// a package from outside its own sources.
type unsafeImporter struct{}

// Import is package unsafe for its path, and an error for any other.
func (unsafeImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	return nil, fmt.Errorf("a shape imports %s, and only unsafe is there to import", path)
}
