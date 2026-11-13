package main

// offlineGoEnvironment is base with the go command held off the network, for every go command a measurement starts:
// the preflight's checks, both release builds and the standard library's tables they generate, and every engine run,
// which starts go list. A rented machine measures with its network off, and a house box measures what the rented
// one would, so neither reaches it.
//
// `GOPROXY=off` stops every module and toolchain fetch. The checksum database is kept out by `GONOSUMDB=*`, which
// matches every module path, and not by `GOSUMDB=off`: under `GOSUMDB=off` go refuses to switch to any toolchain but
// its own, "verifying module: checksum database disabled by GOSUMDB=off", though the toolchain sits in the module
// cache, and the release's tables run every patch release under `GOTOOLCHAIN` (measured on go1.27.0 with go1.27.1
// to go1.27.3 cached, 2026-11-12). `GOSUMDB` is set to its default so a go env file that turns it off cannot.
// Each module is still held to its go.sum line, and each toolchain was verified by --prepare when it was fetched.
func offlineGoEnvironment(base []string) []string {
	return append(environmentWithout(base, "GOPROXY", "GOSUMDB", "GONOSUMDB", "GOPRIVATE"), "GOPROXY=off",
		"GOSUMDB=sum.golang.org", "GONOSUMDB=*")
}

// preparingGoEnvironment is base for --prepare's go commands, which fetch: the network as the machine has it, and the
// checksum database on, so every module and toolchain the cache takes is verified as it arrives. `GOFLAGS` is left
// out as the engine's runs leave it out, so a developer's flags fetch nothing they would not.
func preparingGoEnvironment(base []string) []string {
	return environmentWithout(base, "GOFLAGS")
}
