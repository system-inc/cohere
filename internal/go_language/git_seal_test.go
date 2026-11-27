package go_language

import (
	"os"
	"testing"
)

// cohere-go's own git child reads no git configuration of the machine's in the package's tests (#2htryg8): its
// `git ls-files` at a go.work's root sets no environment of its own, so it inherits TestMain's seal
// (sealedGitEnvironment), under which git reads neither a global file nor a system one. It is the one git child the
// tests have: none starts git itself (internal/testing/guard's TestNoTestUnderGoLanguageStartsGit), so this holds
// the seal without starting one.
func TestCohereGosGitChildInheritsTheTestsSeal(test *testing.T) {
	test.Parallel()

	for name, value := range sealedGitEnvironment {
		if os.Getenv(name) != value {
			test.Errorf("%s is %q in the tests' environment, want %q, as TestMain sets it", name, os.Getenv(name),
				value)
		}
	}
}
