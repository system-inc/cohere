package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// temporaries maps a value to the access path it holds, so `t = props.a; t.b` reads as `props.a.b`.
//
// Lowering breaks every expression into single instructions, so `props.a.b` becomes a `PropertyLoad`
// into a temporary and a second `PropertyLoad` off it. Without this map the second load's object is
// an anonymous value and the dependency would be recorded as that temporary rather than as a path
// through `props`. This is what reassembles them.
type temporaries map[static_single_assignment.IdentifierId]ReactiveScopeDependency

// getProperty resolves one property access into a dependency, upstream's `getProperty`.
//
// When the object is itself a known temporary the access extends that temporary's path; otherwise it
// starts a new path rooted at the object. The extension copies the path rather than appending in
// place, because two accesses off one temporary must not share a backing array.
func (temporaries temporaries) getProperty(object Place, property string, optional bool) ReactiveScopeDependency {
	if resolved, ok := temporaries[object.Identifier]; ok {
		path := make([]DependencyPathEntry, len(resolved.Path), len(resolved.Path)+1)
		copy(path, resolved.Path)
		path = append(path, DependencyPathEntry{Property: property, Optional: optional})
		return ReactiveScopeDependency{
			Identifier: resolved.Identifier,
			Reactive:   resolved.Reactive,
			Path:       path,
		}
	}
	return ReactiveScopeDependency{
		Identifier: object.Identifier,
		Reactive:   object.Reactive,
		Path:       []DependencyPathEntry{{Property: property, Optional: optional}},
	}
}

// resolve returns the dependency a place names, following the temporaries map.
func (temporaries temporaries) resolve(place Place) ReactiveScopeDependency {
	if resolved, ok := temporaries[place.Identifier]; ok {
		path := make([]DependencyPathEntry, len(resolved.Path))
		copy(path, resolved.Path)
		return ReactiveScopeDependency{
			Identifier: resolved.Identifier,
			Reactive:   resolved.Reactive,
			Path:       path,
		}
	}
	return ReactiveScopeDependency{Identifier: place.Identifier, Reactive: place.Reactive}
}

// resolveWithPath resolves a place and appends a suffix path to whatever it resolved to.
func (temporaries temporaries) resolveWithPath(place Place, suffix []DependencyPathEntry) ReactiveScopeDependency {
	base := temporaries.resolve(place)
	if len(suffix) == 0 {
		return base
	}
	path := make([]DependencyPathEntry, 0, len(base.Path)+len(suffix))
	path = append(path, base.Path...)
	path = append(path, suffix...)
	base.Path = path
	return base
}
