// Package reader calls api.Old, a deprecated use in exactly the builds where Old is deprecated.
package reader

import "example.com/deprecatedgroupextra/api"

// Target names only api.Old, whose text and file are the same in every build.
func Target() { api.Old() }
