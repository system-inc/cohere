// Package build_cache stands in for a package that computes a product directory in a build cache, outside every
// package, at a path the rule can't read.
package build_cache

// Product is the directory a product is built into.
func Product(name string) string { return "/var/cache/products/" + name }
