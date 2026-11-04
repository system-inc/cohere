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
func TestAMissingGeneratedFileLeadsAsOneCause(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	routes := "declare global {\n    type PageProps<Route extends string> = { route: Route };\n}\nexport {};\n"
	project := func(t *testing.T, nextEnvironment string, page string) string {
		t.Helper()
		root := t.TempDir()
		writeTree(t, root, map[string]string{
			"tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","types":[],"lib":["es2022"],"skipLibCheck":true},"include":["next-env.d.ts","app"]}`,
			"CohereSettings.json": `{"rules":{}}`,
			".gitignore":          ".next/\n.cache/\n",
			"next-env.d.ts":       nextEnvironment,
			"app/page.ts":         page,
		})
		command := exec.Command("git", "init", "--quiet")
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, output)
		}
		return root
	}
	run := func(t *testing.T, root string, arguments ...string) (string, int) {
		t.Helper()
		command := exec.Command(binary, append([]string{"--no-fix", "--no-format", "--no-cache"}, arguments...)...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err == nil {
			return string(output), 0
		}
		exitError, isExit := err.(*exec.ExitError)
		if !isExit {
			t.Fatalf("running cohere: %v\n%s", err, output)
		}
		return string(output), childExitCode(t, exitError)
	}
	usesPageProps := "export function title(props: PageProps<'/'>): string {\n    return props.route;\n}\n"

	t.Run("missing, then generated", func(t *testing.T) {
		t.Parallel()
		root := project(t, "import \"./.next/types/routes.d.ts\";\n", usesPageProps)

		output, exitCode := run(t, root)
		if exitCode == 0 {
			t.Fatalf("a run missing a generated file passed:\n%s", output)
		}
		if !strings.Contains(output, "next-env.d.ts:1:8 error missing-generated-file ./.next/types/routes.d.ts does not exist") ||
			!strings.Contains(output, "`next typegen` writes it") {
			t.Errorf("the cause and its remedy do not lead:\n%s", output)
		}
		if !strings.Contains(output, "1 type error folded under it, most likely its cascade (1 TS2304)") {
			t.Errorf("the type error is not folded under the cause:\n%s", output)
		}
		if strings.Contains(output, "Cannot find name 'PageProps'") {
			t.Errorf("the default view printed the folded type error:\n%s", output)
		}
		if !strings.Contains(output, "⚠ 1 generated file missing") {
			t.Errorf("the footer does not name the gap:\n%s", output)
		}
		if strings.Index(output, "missing-generated-file") > strings.Index(output, "folded under it") {
			t.Errorf("the cause does not come before the fold:\n%s", output)
		}

		verbose, _ := run(t, root, "--verbose")
		if !strings.Contains(verbose, "missing-generated-file") || !strings.Contains(verbose, "error TS2304: Cannot find name 'PageProps'.") {
			t.Errorf("--verbose does not print the cause and every type error:\n%s", verbose)
		}
		jsonOutput, _ := run(t, root, "--json")
		if !strings.Contains(jsonOutput, `"rule":"missing-generated-file"`) || !strings.Contains(jsonOutput, `"rule":"TS2304"`) ||
			!strings.Contains(jsonOutput, `"missingGenerated":[".next/types/routes.d.ts"]`) {
			t.Errorf("--json does not carry the cause, every type error and the gap:\n%s", jsonOutput)
		}

		writeTree(t, root, map[string]string{".next/types/routes.d.ts": routes})
		output, exitCode = run(t, root)
		if exitCode != 0 || strings.Contains(output, "missing-generated-file") || strings.Contains(output, "generated file missing") {
			t.Errorf("with the generated file present, exit %d:\n%s", exitCode, output)
		}
	})

	t.Run("missing, with no type error following", func(t *testing.T) {
		t.Parallel()
		root := project(t, "import \"./.next/types/routes.d.ts\";\n", "export const title = 'home';\n")
		output, exitCode := run(t, root)
		if exitCode == 0 || !strings.Contains(output, "missing-generated-file") || !strings.Contains(output, "⚠ 1 generated file missing") {
			t.Errorf("a missing generated file with nothing failing after it, exit %d:\n%s", exitCode, output)
		}
		if strings.Contains(output, "folded under it") {
			t.Errorf("a fold line with no type error to fold:\n%s", output)
		}
	})

	// #46ern7e: a writing run holds every lint fix while the file is missing, and applies them once it exists.
	t.Run("fixes held while missing, applied once generated", func(t *testing.T) {
		t.Parallel()
		inferrable := "export const answer: number = 1;\n"
		root := project(t, "import \"./.next/types/routes.d.ts\";\n", inferrable)
		write := func() string {
			t.Helper()
			command := exec.Command(binary, "--no-format", "--no-cache")
			command.Dir = root
			output, _ := command.CombinedOutput()
			return string(output)
		}
		page := func() string {
			t.Helper()
			contents, err := os.ReadFile(filepath.Join(root, "app/page.ts"))
			if err != nil {
				t.Fatal(err)
			}
			return string(contents)
		}

		output := write()
		if page() != inferrable || !strings.Contains(output, "⚠ 🪄 fixes held") {
			t.Errorf("a writing run with a generated file missing changed the page to %q:\n%s", page(), output)
		}

		writeTree(t, root, map[string]string{".next/types/routes.d.ts": routes})
		output = write()
		if page() != "export const answer = 1;\n" || strings.Contains(output, "fixes held") {
			t.Errorf("with the generated file present, the fix did not apply, page %q:\n%s", page(), output)
		}
	})

	t.Run("a reference path", func(t *testing.T) {
		t.Parallel()
		root := project(t, "/// <reference path=\"./.next/types/routes.d.ts\" />\n", usesPageProps)
		output, exitCode := run(t, root)
		if exitCode == 0 || !strings.Contains(output, "next-env.d.ts:1:22 error missing-generated-file ./.next/types/routes.d.ts does not exist") {
			t.Errorf("a missing reference path, exit %d:\n%s", exitCode, output)
		}
	})

	t.Run("missing, and not generated", func(t *testing.T) {
		t.Parallel()
		root := project(t, "import \"./types/routes.d.ts\";\n", usesPageProps)
		output, exitCode := run(t, root)
		if exitCode == 0 || strings.Contains(output, "missing-generated-file") || strings.Contains(output, "folded under it") ||
			!strings.Contains(output, "Cannot find name 'PageProps'") {
			t.Errorf("a missing file git does not ignore is the source's own mistake, exit %d:\n%s", exitCode, output)
		}
		if _, err := os.Stat(filepath.Join(root, "types")); err == nil {
			t.Fatalf("the fixture's missing file exists")
		}
	})
}
