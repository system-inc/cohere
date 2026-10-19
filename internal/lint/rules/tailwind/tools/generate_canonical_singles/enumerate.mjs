/*
 * Ask Tailwind's own canonicalizer what it rewrites a single class to, so enforce-canonical-classes's
 * one-class report is tested against the engine better-tailwindcss calls rather than against a reading of
 * canonicalize-candidates.ts (#15swfzq).
 *
 * Each class goes to `canonicalizeCandidates` alone, with better-tailwindcss 4.7.0's options when its
 * rootFontSize is unset: `{collapse: false, logicalToPhysical: true}`. Collapse is off because a lone
 * class has nothing to merge with, and asking with it on would let a pair rewrite hide behind a single.
 *
 * Every class that compiles is recorded, the ones Tailwind leaves alone as well as the ones it rewrites.
 * The ones it leaves alone are the half that catches over-reporting: a Go printer that spells one class
 * differently from upstream's reports it, and only a recorded "stays the same" can say so.
 *
 * Classes come from two places:
 *
 *   - `cases.json`: upstream's own test inputs for the single-class passes, by describe block, each also
 *     asked under `focus:` and with a trailing `!` as the test file's strategies do, plus the consumer
 *     sites #3dpwm9b and #15swfzq were filed on and the clean cases their bars name.
 *
 *   - `--harvest <root>`: every whitespace-separated token inside a quoted string in a repository's
 *     source, kept when Tailwind compiles it. A token from outside a class literal is harmless here, since
 *     the question is what Tailwind does with the class, not whether the rule reads that string.
 *
 * A class Tailwind cannot compile is never asked, matching better-tailwindcss, which drops unknown classes
 * before calling the canonicalizer because Tailwind crashes on some of them. They are counted.
 *
 * Usage:
 *
 *   node internal/lint/rules/tailwind/tools/generate_canonical_singles/enumerate.mjs <theme.css> \
 *       [--resolve-root <dir>] [--harvest <root>]
 */

import NodeFileSystem from 'node:fs';
import NodePath from 'node:path';
import NodeUrl from 'node:url';
import { loadDesignSystem } from '../generate_descriptors/loader.mjs';
import { locate, recordBeside } from '../corpus.mjs';

const entryPointArgument = process.argv[2];
if (!entryPointArgument) {
    process.stderr.write('usage: enumerate.mjs <theme.css> [--resolve-root <dir>] [--harvest <root>]\n');
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

// better-tailwindcss 4.7.0's call with rootFontSize unset: `rem` stays absent, which is the reason this
// rule never rewrites `h-[76px]` to `h-19`.
const options = { collapse: false, logicalToPhysical: true };

const casesPath = NodePath.join(NodePath.dirname(NodeUrl.fileURLToPath(import.meta.url)), 'cases.json');
const cases = JSON.parse(NodeFileSystem.readFileSync(casesPath, 'utf8'));

/*
 * Each class with where it came from. The first origin wins, so a class that is both an upstream case and
 * a harvested one is reported as the upstream case, which is the more useful name when it disagrees.
 */
const origins = new Map();
function offer(className, origin) {
    if (className && !origins.has(className)) {
        origins.set(className, origin);
    }
}

for (const [block, inputs] of Object.entries(cases.upstream)) {
    for (const input of inputs) {
        offer(input, `upstream: ${block}`);
        // The with-variant and important strategies, which only make sense on a class with no variant of
        // its own already in front of the utility.
        offer(`focus:${input}`, `upstream: ${block}, with-variant`);
        offer(`${input}!`, `upstream: ${block}, important`);
    }
}
for (const input of cases.consumers) {
    offer(input, 'consumer');
}
for (const input of cases.clean) {
    offer(input, 'clean');
}

const harvestArgument = flagValue('--harvest');
let harvested = null;
if (harvestArgument) {
    const harvestRoot = locate(harvestArgument).path;
    harvested = recordBeside(harvestArgument, harvestRoot);
    const skippedDirectories = new Set(['node_modules', '.git', '.next', 'data', 'dist', 'build', '.turbo']);
    const tokens = new Set();
    const visit = (directory) => {
        for (const entry of NodeFileSystem.readdirSync(directory, { withFileTypes: true })) {
            if (skippedDirectories.has(entry.name)) continue;
            const path = NodePath.join(directory, entry.name);
            if (entry.isDirectory()) {
                visit(path);
                continue;
            }
            if (!/\.(tsx?|jsx?|mjs)$/.test(entry.name) || entry.name.endsWith('.d.ts')) continue;
            const source = NodeFileSystem.readFileSync(path, 'utf8');
            for (const match of source.matchAll(/(["'`])((?:(?!\1)[^\\\n]|\\.){1,2000})\1/g)) {
                for (const token of match[2].split(/[\s'"`{}]+/)) {
                    if (token && !token.includes('${')) tokens.add(token);
                }
            }
        }
    };
    visit(harvestRoot);
    for (const token of [...tokens].sort()) {
        offer(token, 'harvested');
    }
}

const recorded = [];
let unknownCount = 0;
for (const [className, origin] of origins) {
    const isKnown = designSystem.parseCandidate(className).length > 0 && designSystem.candidatesToCss([className])[0] !== null;
    if (!isKnown) {
        unknownCount++;
        continue;
    }
    const canonical = designSystem.canonicalizeCandidates([className], options);
    recorded.push({ className, origin, canonical: canonical.join(' ') });
}

process.stdout.write(
    JSON.stringify(
        {
            tailwindVersion,
            entryPoint: recordedEntryPoint,
            harvested,
            options,
            groundTruth: 'canonicalizeCandidates([className], options), one class at a time',
            unknownCount,
            caseCount: recorded.length,
            rewriteCount: recorded.filter((entry) => entry.canonical !== entry.className).length,
            cases: recorded,
        },
        null,
        2,
    ) + '\n',
);
