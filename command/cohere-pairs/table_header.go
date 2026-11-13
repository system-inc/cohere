package main

// tableHeader is what the table says above its runs: the two binaries and the host they ran on.
type tableHeader struct {
	base              builtBinary
	branch            builtBinary
	host              string
	goVersion         string
	goOperatingSystem string
	goArchitecture    string
	cores             int
	rounds            int
}
