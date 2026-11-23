package merged

import (
	"time"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/variational_types/system_cache"
)

// SystemCache is the source of one build that reads the engine's system cache through build: each package read
// whole from its shallow export data, every package it names first, into build's Packages. A package it holds is
// complete, so Reached gives only those.
func SystemCache(build *system_cache.Build) Source {
	return &systemCache{build: build}
}

// systemCache is SystemCache's source.
type systemCache struct {
	build   *system_cache.Build
	elapsed time.Duration
}

func (source *systemCache) Import(path string) (*types.Package, error) {
	started := time.Now()
	defer func() { source.elapsed += time.Since(started) }()
	present, readError := source.build.Read(path)
	if readError != nil || !present {
		return nil, readError
	}
	return source.build.Packages[path], nil
}

func (source *systemCache) Reached(path string) *types.Package { return source.build.Packages[path] }

func (source *systemCache) Elapsed() time.Duration { return source.elapsed }
