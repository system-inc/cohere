# cohere for VS Code

Format on save with cohere, replacing the Prettier extension. A save runs
`cohere --fix --format --stdin-filepath <file>` on the buffer and applies what comes back, which is
exactly what `cohere --fix --format <file>` would write for the same text: the same lint fixes, the
same printers, the same options and ignore layers. The editor and the gate cannot disagree.

The edit is applied before the save lands, through VS Code's formatter API, so the document does not
reload and undo works as usual.

## What a save does

- **Formats and fixes:** the buffer comes back fixed and formatted.
- **Leaves alone, silently:** a file cohere declines comes back as typed. That covers a type with no
  printer, a file outside the format scope, and source that does not parse; the gate reports a typo,
  the save does not.
- **Shows an error:** a broken formatter, a project cohere cannot load, or a missing binary shows an
  error, and the buffer saves as typed.

## Which cohere

The same one `s c` runs, found in Structure's order:

1. `COHERE_BINARY`.
2. The project's `node_modules/.bin/cohere`.
3. `cohere` on `PATH`.

A save runs from the file's own directory, so cohere finds the nearest project, as `s c` would run
from there.

## Install

From a checkout, link the folder into VS Code's extensions and reload the window:

```sh
ln -s ~/Projects/system/cohere/editors/vscode ~/.vscode/extensions/system-inc.cohere-0.0.1
```

Then set it as the formatter (`s doctor` writes these):

```json
"editor.defaultFormatter": "system-inc.cohere",
"editor.formatOnSave": true
```

## Test

```sh
node --test cohere-format.test.js                                  # resolution and failure handling
COHERE_BINARY=/path/to/cohere node --test cohere-format.test.js     # plus a real save against the gate
```

`extension.js` is the VS Code glue around `cohere-format.js` and is not covered by these tests.
