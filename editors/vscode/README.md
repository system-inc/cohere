# cohere for VS Code

Format on save with cohere, replacing the Prettier extension. A save runs
`cohere --fix --format --stdin-filepath <file>` on the buffer and applies what comes back, which is
exactly what `cohere --fix --format <file>` would write for the same text: the same lint fixes, the
same printers, the same options and ignore layers. The editor and the gate cannot disagree.

The edit is applied before the save lands, through VS Code's formatter API, so the document does not
reload and undo works as usual.

## What a save does

The formatter is registered only for the file types cohere fixes or formats through stdin, listed in
`cohere-save.json`: TypeScript, JavaScript, Adamic, JSON, Markdown, CSS, GraphQL, YAML and Go. A `.mts` or
`.cts` file is fixed but not yet formatted, since no printer handles it. A Go file in a Go module or workspace is
formatted by gofmt and has its long lines wrapped; a rename a naming rule asks for spans files, so a save leaves it
for a run over the module. Any other file never reaches cohere, so its save does nothing and says nothing.

- **Formats and fixes:** the buffer comes back fixed and formatted.
- **Leaves alone, silently:** a file cohere declines comes back as typed. That covers a file outside
  the format scope or ignored by git, source that does not parse, and a file in a project cohere has no
  stdin path for yet, a Swift package; the gate reports a typo, the save does not.
- **Shows an error:** a broken formatter, a project cohere cannot load, or a missing binary shows an
  error, and the buffer saves as typed.

## Which cohere

The same one a bare `c` runs in that folder, found the same way:

1. `COHERE_BINARY`.
2. The project's `node_modules/.bin/cohere`.
3. `cohere` on `PATH`.

A save runs from the file's own directory, so cohere finds the nearest project, as a bare `c` run from
there would.

## Install

Install **cohere** from the Extensions view: it's published to the Visual Studio Marketplace for VS
Code, and to Open VSX for Cursor, VSCodium and Windsurf. Both update it for you. The extension is
versioned with cohere itself, so cohere 1.0.0 pairs with extension 1.0.0.

A `.vsix` from a release installs the same way:

```sh
code --install-extension cohere-1.0.0.vsix
```

Then set it as the formatter (`s doctor` writes these):

```json
"editor.defaultFormatter": "system-inc.cohere",
"editor.formatOnSave": true
```

The extension runs cohere and does not bundle it. Install cohere in the project with
`pnpm add -D @system-inc/cohere`. When no cohere is found, the first save in a project says so, names
where it looked, and offers to copy that command. The file saves as typed.

## Test

```sh
node --test cohere-format.test.js                                  # resolution and failure handling
COHERE_BINARY=/path/to/cohere node --test cohere-format.test.js     # plus a real save against the gate
```

The extension itself is tested in a real VS Code, from its `.vsix`, in a profile with nothing else in
it (macOS, VS Code in `/Applications`; it opens windows while it runs):

```sh
bash ../../.github/scripts/package-vscode-extension.sh 1.0.0 /tmp/vsix
node test-vscode/run.js /tmp/vsix/cohere-1.0.0.vsix ~/Projects/ahra "$(command -v cohere)"
```

It saves a misformatted file in the project and requires the bytes on disk to equal what
`cohere --fix --format` writes for the same text. Then it saves a file where no cohere can be found
and requires it saved as typed, and in a Go module saves a misformatted Go file, which must come back as gofmt
prints it, and a TypeScript file, which the Go engine declines there, and requires it saved as typed.
