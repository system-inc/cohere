// Package dotted declares a variable that a dot import brings in bare, so its name reads as the importer's own.
package dotted

// Value is this package's to name, wherever it is read.
var Value = struct{ Value int }{Value: 1}
