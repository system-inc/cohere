package main

import (
	"slices"
	"testing"
)

// Every go command the pair starts runs off the network: the proxy off and the checksum database skipped for every
// module, whatever the machine's environment said. The database is skipped by `GONOSUMDB=*` and not turned off by
// `GOSUMDB=off`, under which go refuses to switch to a cached toolchain (offlineGoEnvironment), so `GOSUMDB` must not
// be off. An inherited proxy or database setting is replaced, never left beside, since go reads the last.
func TestOfflineGoEnvironmentHoldsGoOffTheNetwork(test *testing.T) {
	test.Parallel()
	environment := offlineGoEnvironment([]string{"HOME=/home/job", "GOPROXY=https://proxy.golang.org,direct",
		"GOSUMDB=off", "GONOSUMDB=example.com", "GOPRIVATE=example.com", "GOFLAGS=-mod=mod"})
	for _, wanted := range []string{"GOPROXY=off", "GONOSUMDB=*", "GOSUMDB=sum.golang.org", "HOME=/home/job",
		"GOFLAGS=-mod=mod"} {
		if !slices.Contains(environment, wanted) {
			test.Errorf("the environment lacks %s: %q", wanted, environment)
		}
	}
	for _, unwanted := range []string{"GOPROXY=https://proxy.golang.org,direct", "GOSUMDB=off",
		"GONOSUMDB=example.com", "GOPRIVATE=example.com"} {
		if slices.Contains(environment, unwanted) {
			test.Errorf("the environment keeps %s: %q", unwanted, environment)
		}
	}
	if preparing := preparingGoEnvironment([]string{"GOFLAGS=-mod=mod", "GOPROXY=direct"}); !slices.Equal(preparing,
		[]string{"GOPROXY=direct"}) {
		test.Errorf("--prepare's environment is %q, want the machine's less GOFLAGS", preparing)
	}
}
