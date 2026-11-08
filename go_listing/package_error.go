package go_listing

// A PackageError is an error go list reports for a package. go list names the text Err and the
// position Pos, relative to the directory it ran in when the file is under it.
type PackageError struct {
	Position    string `json:"Pos"`
	Message     string `json:"Err"`
	ImportStack []string
}

// Error is the error as go prints it: its position, when it has one, then its text.
func (packageError *PackageError) Error() string {
	if packageError.Position == "" {
		return packageError.Message
	}
	return packageError.Position + ": " + packageError.Message
}
