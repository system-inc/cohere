package platform

import (
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// ObjectDigests is how one build reads every object of every package outside the workspace its load reaches, the
// standard library and module dependencies alike: by the object's key, the digest of its rendering as check's export
// rendering writes it and of every fact the registry's fact analyzers attach to it. A package's own facts are under
// its key with an empty Path. Two builds read an object alike only when both hold its key with the same digest, so an
// object one build holds and the other does not differs.
type ObjectDigests map[ObjectKey]cache.Key

// PackageDigests is what every workspace package of one build gives its importers, by its load ID, test variants
// included: its export and the facts it exports, as check's output digest combines them.
type PackageDigests map[string]cache.Key
