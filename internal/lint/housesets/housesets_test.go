package housesets

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/types/program"
)

// Detection is from the program, per file (#j6p9t1g): each case is a project on disk, built the way a run
// builds it, so the imports and the JSX fact are the compiler's own.

func buildProject(test *testing.T, files map[string]string) (string, *program.Graph) {
	test.Helper()
	root := test.TempDir()
	// A copy takes the default tsconfig, so the caller's map holds only what the caller put in it.
	fixture := maps.Clone(files)
	if _, written := fixture["tsconfig.json"]; !written {
		fixture["tsconfig.json"] = `{"compilerOptions": {"jsx": "preserve", "strict": true, "noEmit": true, "skipLibCheck": true}, "include": ["**/*"]}`
	}
	for name, contents := range fixture {
		path := filepath.Join(root, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json"), CurrentDirectory: root})
	if buildError != nil {
		test.Fatal(buildError)
	}
	return root, graph
}

func TestReactIsDetectedFromImportsAndJsxNotFromPackageJson(test *testing.T) {
	test.Parallel()
	root, graph := buildProject(test, map[string]string{
		// react is a dependency, and that alone applies nothing.
		"package.json":             `{"dependencies": {"react": "19.2.0", "next": "16.0.0"}, "scripts": {"react": "echo react"}}`,
		"source/Imports.tsx":       "import { useState } from 'react';\nexport function useCount() { return useState(0); }\n",
		"source/Dom.ts":            "import { createRoot } from 'react-dom/client';\nexport const make = createRoot;\n",
		"source/JsxOnly.tsx":       "export function Badge() { return <span>new</span>; }\n",
		"source/NoJsx.tsx":         "export const answer = 42;\n",
		"server/index.ts":          "export function handle(request: Request): Response { return new Response(request.url); }\n",
		"source/Reactive.ts":       "import { reactive } from './reactive-store';\nexport const store = reactive;\n",
		"source/reactive-store.ts": "export const reactive = 1;\n",
	})
	detection := Detect(graph.ProjectFiles(), root, graph.Program.Host().FS(), nil, "")

	want := map[string]bool{"source/Imports.tsx": true, "source/Dom.ts": true, "source/JsxOnly.tsx": true}
	for _, file := range graph.ProjectFiles() {
		relative, relativeError := filepath.Rel(root, file.FileName().AsString())
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		if detection.ReactFiles[file.FileName().AsString()] != want[relative] {
			test.Errorf("%s: react=%v, want %v", relative, detection.ReactFiles[file.FileName().AsString()], want[relative])
		}
	}
	// next is in package.json and imported nowhere, so no file is Next's.
	if len(detection.NextFiles) != 0 {
		test.Errorf("next applied with no import of it: %v", detection.NextFiles)
	}
}

func TestNextIsDetectedFromImportsAndItsOwnFilesOnlyInANextProgram(test *testing.T) {
	test.Parallel()
	files := map[string]string{
		"app/layout.tsx":                 "import type { Metadata } from 'next';\nexport const metadata: Metadata = {};\n",
		"app/page.tsx":                   "export default function Page() { return <main />; }\n",
		"app/api/health/route.ts":        "export function GET() { return new Response('ok'); }\n",
		"middleware.ts":                  "export function middleware() {}\n",
		"source/Link.tsx":                "import Link from 'next/link';\nexport const Home = () => <Link href=\"/\">home</Link>;\n",
		"server/worker.ts":               "export const port = 8080;\n",
		"node_modules/next/index.d.ts":   "export interface Metadata { title?: string }\n",
		"node_modules/next/link.d.ts":    "declare const Link: (properties: { href: string; children?: unknown }) => unknown;\nexport default Link;\n",
		"node_modules/next/package.json": `{"name": "next", "types": "index.d.ts"}`,
	}
	root, graph := buildProject(test, files)
	detection := Detect(graph.ProjectFiles(), root, graph.Program.Host().FS(), nil, "")
	want := map[string]bool{"app/layout.tsx": true, "app/page.tsx": true, "app/api/health/route.ts": true, "middleware.ts": true, "source/Link.tsx": true}
	for _, file := range graph.ProjectFiles() {
		relative, relativeError := filepath.Rel(root, file.FileName().AsString())
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		if strings.HasPrefix(relative, "node_modules") {
			continue
		}
		if detection.NextFiles[file.FileName().AsString()] != want[relative] {
			test.Errorf("%s: next=%v, want %v", relative, detection.NextFiles[file.FileName().AsString()], want[relative])
		}
	}

	// The control: the same app/ directory in a program that never imports next is just a directory.
	delete(files, "app/layout.tsx")
	delete(files, "source/Link.tsx")
	root, graph = buildProject(test, files)
	if detection := Detect(graph.ProjectFiles(), root, graph.Program.Host().FS(), nil, ""); len(detection.NextFiles) != 0 {
		test.Errorf("an app/ directory with no import of next applied cohere:next: %v", detection.NextFiles)
	}
}

func TestTailwindIsDetectedFromTheStylesheetAndSkippedByNameWithoutOne(test *testing.T) {
	test.Parallel()
	root, graph := buildProject(test, map[string]string{
		"index.ts":        "export const x = 1;\n",
		"app/globals.css": "@import \"tailwindcss\";\n",
	})
	detection := Detect(graph.ProjectFiles(), root, graph.Program.Host().FS(), nil, "")
	if detection.TailwindEntryPoint != filepath.Join(root, "app", "globals.css") {
		test.Errorf("entry %q, want app/globals.css", detection.TailwindEntryPoint)
	}

	root, graph = buildProject(test, map[string]string{
		"index.ts":        "export const x = 1;\n",
		"app/globals.css": "body { margin: 0; }\n",
	})
	detection = Detect(graph.ProjectFiles(), root, graph.Program.Host().FS(), nil, "")
	if detection.TailwindEntryPoint != "" || !strings.Contains(detection.TailwindSkipped, "app/globals.css does not import tailwindcss") {
		test.Errorf("a plain stylesheet applied tailwind: %+v", detection)
	}

	root, graph = buildProject(test, map[string]string{"index.ts": "export const x = 1;\n"})
	detection = Detect(graph.ProjectFiles(), root, graph.Program.Host().FS(), nil, "")
	if detection.TailwindEntryPoint != "" || !strings.Contains(detection.TailwindSkipped, "no Tailwind stylesheet") {
		test.Errorf("no stylesheet must skip tailwind by name: %+v", detection)
	}
}
