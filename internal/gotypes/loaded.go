package gotypes

// Loaded is one load: the packages the patterns name, and every package read from source, the named ones
// among them, in dependency order, followed by any test variants Config.Tests asked for.
type Loaded struct {
	Roots  []*Package
	Source []*Package
}

// ErrorsAnywhere is every error of every package read from source, each once, in dependency order.
func (loaded *Loaded) ErrorsAnywhere() []error {
	var found []error
	for _, loadedPackage := range loaded.Source {
		found = append(found, loadedPackage.Errors...)
	}
	return found
}
