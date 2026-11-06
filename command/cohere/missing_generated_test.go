package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAMissingGeneratedFileLeadsAsOneCause is #wxsqavy end to end, both ways. A Next project whose
// next-env.d.ts imports .next/types/routes.d.ts, which git ignores, before `next typegen` has written it:
// the run leads with that one file and its remedy, folds the type errors under it in the default view,
// prints every one under --verbose, names the gap in the footer and fails. With the file written, the same
// project runs clean.
//
// The cases around it hold the edges: a missing generated file with no type error following still fails,
// since what it declares was never checked; a writing run holds every lint fix while it is missing; a
// reference path is the same trigger as an import; and a missing file git does not ignore is a mistake in
// the source, left to its own type error.
//
// skipLibCheck is on, as create-next-app writes it. With it off the checker also reports the import itself
// (TS2882, next-env.d.ts being a declaration file it then checks), which folds with the rest.
func TestAMissingGeneratedFileLeadsAsOneCause(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	routes := "declare global {\n    type PageProps<Route extends string> = { route: Route };\n}\nexport {};\n"
	project := func(test *testing.T, nextEnvironment string, page string) string {
		test.Helper()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","types":[],"lib":["es2022"],"skipLibCheck":true},"include":["next-env.d.ts","app"]}`,
			"CohereSettings.json": `{"rules":{}}`,
			".gitignore":          ".next/\n.cache/\n",
			"next-env.d.ts":       nextEnvironment,
			"app/page.ts":         page,
		})
		command := exec.Command("git", "init", "--quiet")
		command.Dir = root
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("git init: %v\n%s", outputError, output)
		}
		return root
	}
	run := func(test *testing.T, root string, arguments ...string) (string, int) {
		test.Helper()
		command := exec.Command(binary, append([]string{"--no-fix", "--no-format", "--no-cache"}, arguments...)...)
		command.Dir = root
		output, outputError := command.CombinedOutput()
		if outputError == nil {
			return string(output), 0
		}
		exitError, isExit := outputError.(*exec.ExitError)
		if !isExit {
			test.Fatalf("running cohere: %v\n%s", outputError, output)
		}
		return string(output), childExitCode(test, exitError)
	}
	usesPageProperties := "export function title(props: PageProps<'/'>): string {\n    return props.route;\n}\n"

	test.Run("missing, then generated", func(test *testing.T) {
		test.Parallel()
		root := project(test, "import \"./.next/types/routes.d.ts\";\n", usesPageProperties)

		output, exitCode := run(test, root)
		if exitCode == 0 {
			test.Fatalf("a run missing a generated file passed:\n%s", output)
		}
		if !strings.Contains(
			output,
			"next-env.d.ts:1:8 error missing-generated-file ./.next/types/routes.d.ts does not exist",
		) ||
			!strings.Contains(output, "`next typegen` writes it") {
			test.Errorf("the cause and its remedy do not lead:\n%s", output)
		}
		if !strings.Contains(output, "1 type error folded under it, most likely its cascade (1 TS2304)") {
			test.Errorf("the type error is not folded under the cause:\n%s", output)
		}
		if strings.Contains(output, "Cannot find name 'PageProps'") {
			test.Errorf("the default view printed the folded type error:\n%s", output)
		}
		if !strings.Contains(output, "⚠ 1 generated file missing") {
			test.Errorf("the footer does not name the gap:\n%s", output)
		}
		if strings.Index(output, "missing-generated-file") > strings.Index(output, "folded under it") {
			test.Errorf("the cause does not come before the fold:\n%s", output)
		}

		verbose, _ := run(test, root, "--verbose")
		if !strings.Contains(verbose, "missing-generated-file") ||
			!strings.Contains(verbose, "error TS2304: Cannot find name 'PageProps'.") {
			test.Errorf("--verbose does not print the cause and every type error:\n%s", verbose)
		}
		jsonOutput, _ := run(test, root, "--json")
		if !strings.Contains(jsonOutput, `"rule":"missing-generated-file"`) ||
			!strings.Contains(jsonOutput, `"rule":"TS2304"`) ||
			!strings.Contains(jsonOutput, `"missingGenerated":[".next/types/routes.d.ts"]`) {
			test.Errorf("--json does not carry the cause, every type error and the gap:\n%s", jsonOutput)
		}

		writeTree(test, root, map[string]string{".next/types/routes.d.ts": routes})
		output, exitCode = run(test, root)
		if exitCode != 0 ||
			strings.Contains(output, "missing-generated-file") ||
			strings.Contains(output, "generated file missing") {
			test.Errorf("with the generated file present, exit %d:\n%s", exitCode, output)
		}
	})

	test.Run("missing, with no type error following", func(test *testing.T) {
		test.Parallel()
		root := project(test, "import \"./.next/types/routes.d.ts\";\n", "export const title = 'home';\n")
		output, exitCode := run(test, root)
		if exitCode == 0 ||
			!strings.Contains(output, "missing-generated-file") ||
			!strings.Contains(output, "⚠ 1 generated file missing") {
			test.Errorf("a missing generated file with nothing failing after it, exit %d:\n%s", exitCode, output)
		}
		if strings.Contains(output, "folded under it") {
			test.Errorf("a fold line with no type error to fold:\n%s", output)
		}
	})

	// #46ern7e: a writing run holds every lint fix while the file is missing, and applies them once it exists.
	test.Run("fixes held while missing, applied once generated", func(test *testing.T) {
		test.Parallel()
		inferrable := "export const answer: number = 1;\n"
		root := project(test, "import \"./.next/types/routes.d.ts\";\n", inferrable)
		write := func() string {
			test.Helper()
			command := exec.Command(binary, "--no-format", "--no-cache")
			command.Dir = root
			output, runError := command.CombinedOutput()
			requireChildRan(test, runError)
			return string(output)
		}
		page := func() string {
			test.Helper()
			contents, readError := os.ReadFile(filepath.Join(root, "app/page.ts"))
			if readError != nil {
				test.Fatal(readError)
			}
			return string(contents)
		}

		output := write()
		if page() != inferrable || !strings.Contains(output, "⚠ 🪄 fixes held") {
			test.Errorf("a writing run with a generated file missing changed the page to %q:\n%s", page(), output)
		}

		writeTree(test, root, map[string]string{".next/types/routes.d.ts": routes})
		output = write()
		if page() != "export const answer = 1;\n" || strings.Contains(output, "fixes held") {
			test.Errorf("with the generated file present, the fix did not apply, page %q:\n%s", page(), output)
		}
	})

	test.Run("a reference path", func(test *testing.T) {
		test.Parallel()
		root := project(test, "/// <reference path=\"./.next/types/routes.d.ts\" />\n", usesPageProperties)
		output, exitCode := run(test, root)
		if exitCode == 0 || !strings.Contains(
			output,
			"next-env.d.ts:1:22 error missing-generated-file ./.next/types/routes.d.ts does not exist",
		) {
			test.Errorf("a missing reference path, exit %d:\n%s", exitCode, output)
		}
	})

	test.Run("missing, and not generated", func(test *testing.T) {
		test.Parallel()
		root := project(test, "import \"./types/routes.d.ts\";\n", usesPageProperties)
		output, exitCode := run(test, root)
		if exitCode == 0 ||
			strings.Contains(output, "missing-generated-file") ||
			strings.Contains(output, "folded under it") ||
			!strings.Contains(output, "Cannot find name 'PageProps'") {
			test.Errorf(
				"a missing file git does not ignore is the source's own mistake, exit %d:\n%s",
				exitCode,
				output,
			)
		}
		if _, statError := os.Stat(filepath.Join(root, "types")); statError == nil {
			test.Fatalf("the fixture's missing file exists")
		}
	})
}
