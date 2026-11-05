package configuration

import (
	"strings"
	"testing"
)

// TestZeroMatchOverrideSelectorIsRefused is the failure this guard exists to expose.
//
// The valid selector beside the miss is load-bearing: a validator that checks the files array as a
// single OR would see modules/** match and let the stale generated-code selector remain silent.
func TestZeroMatchOverrideSelectorIsRefused(test *testing.T) {
	test.Parallel()
	configuration := &Config{
		Root: "/repo",
		Overrides: []Override{{
			Files: []string{"modules/**", "**/generated-miss/**/*.ts"},
		}},
	}

	validateSelectorsError := configuration.ValidateSelectors([]string{
		"/repo/index.ts",
		"/repo/modules/finance/Sync.ts",
		"/repo/libraries/api/generated/Operations.ts",
	})
	if validateSelectorsError == nil {
		test.Fatal("a config with one live selector and one zero-match selector validated, so the live " +
			"one hid the vacuous one")
	}
	if !strings.Contains(validateSelectorsError.Error(), "generated-miss") {
		test.Errorf("the error does not name the selector a reader must fix: %v", validateSelectorsError)
	}
	if strings.Contains(validateSelectorsError.Error(), `"modules/**"`) {
		test.Errorf("the error names a selector that did match: %v", validateSelectorsError)
	}
}

// TestEveryMatchingSelectorPasses is the other direction. Without it a validator that rejects every
// override would satisfy the failure case above while making all real configurations unusable.
func TestEveryMatchingSelectorPasses(test *testing.T) {
	test.Parallel()
	configuration := &Config{
		Root: "/repo",
		Overrides: []Override{
			{Files: []string{"**/generated/**/*.{ts,tsx}"}},
			{Files: []string{"**/next-env.d.ts"}},
			{Files: []string{"modules/**"}},
		},
	}

	validateSelectorsError := configuration.ValidateSelectors([]string{
		"/repo/libraries/api/generated/Operations.ts",
		"/repo/next-env.d.ts",
		"/repo/modules/finance/Sync.ts",
	})
	if validateSelectorsError != nil {
		test.Fatalf("the three live selector shapes did not validate against files each one reaches: %v", validateSelectorsError)
	}
}

// TestASelectorReachingOnlyIgnoredFilesIsRefused pins the effective population rather than the disk
// population. An override cannot affect a file excluded before override resolution, so matching one
// must not make the selector look alive.
func TestASelectorReachingOnlyIgnoredFilesIsRefused(test *testing.T) {
	test.Parallel()
	configuration := &Config{
		Root:           "/repo",
		IgnorePatterns: []string{"generated/**"},
		Overrides: []Override{{
			Files: []string{"generated/**"},
		}},
	}

	validateSelectorsError := configuration.ValidateSelectors([]string{
		"/repo/index.ts",
		"/repo/generated/Operations.ts",
	})
	if validateSelectorsError == nil {
		test.Fatal("a selector reaching only globally ignored files validated even though no rule can run there")
	}
	if !strings.Contains(validateSelectorsError.Error(), "1 non-ignored project files") {
		test.Errorf("the error does not state the population it actually checked: %v", validateSelectorsError)
	}
}

// TestAnOverrideWithoutSelectorsIsRefused covers the zero-length form of the same defect. There is
// no pattern to misspell, but the block can never apply and previously disappeared just as quietly.
func TestAnOverrideWithoutSelectorsIsRefused(test *testing.T) {
	test.Parallel()
	configuration := &Config{Overrides: []Override{{}}}

	validateSelectorsError := configuration.ValidateSelectors([]string{"index.ts"})
	if validateSelectorsError == nil {
		test.Fatal("an override with no files patterns validated even though it cannot select a file")
	}
	if !strings.Contains(validateSelectorsError.Error(), "override 0 with no files patterns") {
		test.Errorf("the error does not identify the empty override: %v", validateSelectorsError)
	}
}
