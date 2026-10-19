/*
 * Record Tailwind's own utility signature for every class arbitraryUtilities searches, the oracle the Go
 * signature is held to (#15swfzq unit 1b).
 *
 * A signature is upstream's normal form of what a class compiles to: `.x { @apply <class> }` with theme
 * variables inlined, `calc()` folded, declarations deduplicated and sorted, and with `collapse` on, every
 * shorthand expanded. Two classes are interchangeable to the canonicalizer exactly when their signatures
 * are equal, so the Go port has to reproduce the string, not an approximation of it.
 *
 * # Read from the engine's own cache
 *
 * The signature function is not exported. `canonicalizeCandidates` fills the design system's `storage`
 * with symbol-keyed caches, and the two read here are found by shape rather than by position: the
 * signature cache maps a class to a string beginning `.x {`, and the precomputed lookup maps a signature
 * to the classes sharing it. The precomputed lookup is the population: every listed class with its
 * non-numeric modifiers, less a `-x-0` whose signature equals `x-0`'s, which is exactly what the pass
 * searches.
 *
 * # What is recorded
 *
 * One line per class: the name, the first twelve hex digits of the SHA-256 of its signature under the
 * plugin's default options (`collapse` and `logicalToPhysical` on), the same with `collapse` off, and the
 * property names the default signature declares. A hash keeps three themes' worth of signatures, 7.6 MB
 * of text on ahra alone, to a size worth committing; the Go side hashes its own string the same way. The
 * property names are what let the Go side check the relation it answers for a class it cannot compile:
 * that class must never be ruled out of a pool its real signature belongs to.
 *
 * Lines for the classes a repository writes as targets follow, from `--targets <json>`: the cases of a
 * canonical_singles capture, the classes the pass would be asked about.
 *
 * Usage:
 *
 *   node internal/lint/rules/tailwind/tools/generate_signatures/signatures.mjs <theme.css> \
 *       [--resolve-root <dir>] [--targets <canonical_singles.json>] > signatures.txt.gz
 */

import NodeCrypto from 'node:crypto';
import NodeFileSystem from 'node:fs';
import NodeZlib from 'node:zlib';
import { loadDesignSystem } from '../generate_descriptors/loader.mjs';

const entryPointArgument = process.argv[2];
if (!entryPointArgument) {
    process.stderr.write('usage: signatures.mjs <theme.css> [--resolve-root <dir>] [--targets <json>]\n');
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

/*
 * The caches for one set of options. `bg-[#000]` is an arbitrary value, so canonicalizing it reaches
 * arbitraryUtilities, which builds the precomputed lookup and with it a signature for every listed class.
 */
function cachesFor(options) {
    designSystem.canonicalizeCandidates(['bg-[#000]'], options);
    let signatures = null;
    let lookup = null;
    for (const symbol of Object.getOwnPropertySymbols(designSystem.storage)) {
        const byOptions = designSystem.storage[symbol];
        if (!byOptions || typeof byOptions.get !== 'function' || typeof byOptions.entries !== 'function') continue;
        for (const [key, inner] of byOptions.entries()) {
            if (!key || typeof key !== 'object' || !('features' in key) || !('rem' in key)) continue;
            if (key.features !== features(options)) continue;
            if (!inner || typeof inner.get !== 'function' || typeof inner.entries !== 'function') continue;
            const sample = inner.entries().next().value;
            if (!sample) continue;
            if (typeof sample[1] === 'string' && sample[1].startsWith('.x {')) signatures = inner;
            else if (typeof sample[0] === 'string' && sample[0].startsWith('.x {') && Array.isArray(sample[1])) lookup = inner;
        }
    }
    if (!signatures || !lookup) {
        process.stderr.write('signatures.mjs: the signature cache or the precomputed lookup was not found in storage\n');
        process.exit(1);
    }
    return { signatures, lookup };
}

// upstream's SignatureFeatures: ExpandProperties is 1 and LogicalToPhysical 2.
function features(options) {
    return (options.collapse ? 1 : 0) | (options.logicalToPhysical ? 2 : 0);
}

const collapsed = cachesFor({ collapse: true, logicalToPhysical: true });
const uncollapsed = cachesFor({ collapse: false, logicalToPhysical: true });

function hash(signature) {
    if (typeof signature !== 'string') return '-';
    return NodeCrypto.createHash('sha256').update(signature).digest('hex').slice(0, 12);
}

function properties(signature) {
    if (typeof signature !== 'string') return '-';
    const names = new Set();
    // Declarations only: a line ending in a semicolon that opens no block, so an at-rule's condition,
    // `@supports (color: red) {`, is not read as a property.
    for (const match of signature.matchAll(/^\s*([^\s{}@][^:{}]*?): [^{\n]*;$/gm)) names.add(match[1]);
    return [...names].sort().join(' ');
}

function line(className) {
    const signature = collapsed.signatures.get(className);
    return `${className}\t${hash(signature)}\t${hash(uncollapsed.signatures.get(className))}\t${properties(signature)}`;
}

const listed = [];
for (const classNames of collapsed.lookup.values()) listed.push(...classNames);
listed.sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));

const lines = [`# tailwind ${tailwindVersion} entry ${recordedEntryPoint}`, '# listed'];
for (const className of listed) lines.push(line(className));

const targetsPath = flagValue('--targets');
if (targetsPath) {
    const listedSet = new Set(listed);
    const targets = JSON.parse(NodeFileSystem.readFileSync(targetsPath, 'utf8'))
        .cases.map((entry) => entry.className)
        .filter((className) => !listedSet.has(className))
        .sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));
    lines.push('# targets');
    for (const className of targets) lines.push(line(className));
}

const text = lines.join('\n') + '\n';
process.stdout.write(NodeZlib.gzipSync(Buffer.from(text, 'utf8'), { level: 9 }));
