/*
 * Record one design system's `getClassList()`, the oracle BuildClassList is held to (#15swfzq unit 1b).
 *
 * One line per class, `name<TAB>modifiers`, the modifiers space-separated and sorted, the lines sorted
 * by name, gzipped. A text line per class rather than JSON keeps a diff readable after `gunzip`, and
 * gzip keeps ahra's 37,645 classes, most of them a colour name times twenty-one opacity modifiers, to
 * a size worth committing.
 *
 * The first line is a header naming the Tailwind version and the entry point, which the test reads to
 * load the same design system the list was captured from.
 *
 * Usage:
 *
 *   node internal/lint/rules/tailwind/tools/generate_suggestions/class_list.mjs <theme.css> [--resolve-root <dir>] > class_list.txt.gz
 */

import NodeZlib from 'node:zlib';
import { loadDesignSystem } from '../generate_descriptors/loader.mjs';

const entryPointArgument = process.argv[2];
if (!entryPointArgument) {
    process.stderr.write('usage: class_list.mjs <theme.css> [--resolve-root <dir>]\n');
    process.exit(2);
}

function flagValue(name) {
    const index = process.argv.indexOf(name);
    return index === -1 ? undefined : process.argv[index + 1];
}

const { designSystem, tailwindVersion, recordedEntryPoint } = await loadDesignSystem(
    entryPointArgument,
    flagValue('--resolve-root'),
);

const lines = designSystem
    .getClassList()
    .map(([name, meta]) => `${name}\t${[...meta.modifiers].sort().join(' ')}`)
    .sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));

const text = [`# tailwind ${tailwindVersion} entry ${recordedEntryPoint}`, ...lines].join('\n') + '\n';
process.stdout.write(NodeZlib.gzipSync(Buffer.from(text, 'utf8'), { level: 9 }));
