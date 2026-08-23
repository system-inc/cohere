/*
 * Enumerate every collapse family Tailwind knows, by asking the real engine.
 *
 * This is the expensive half of `enforce-canonical-classes` reduced to data. Tailwind's
 * `canonicalizeCandidates` is a signature-equivalence search: it compiles each candidate to CSS,
 * builds a property-to-value signature, enumerates every other utility root, reprints under each,
 * intersects, then enumerates subsets. Porting that search to Go is eight to ten thousand lines and
 * it changes every Tailwind minor.
 *
 * The search is expensive. Its answers are not. Measured on Tailwind 4.3.3: 327 functional roots
 * yield 53,301 pairs, of which 38 collapse, and the families are value-independent, so
 * `px + py => p` is a fact about roots rather than a computation per class. Go needs the table.
 *
 * Two properties make the table safe, and both are why this enumerates rather than samples:
 *
 *   The registry is the population, not the corpus. `getClassList()` returns every utility the
 *   design system knows. An earlier attempt enumerated from the classes this codebase happens to
 *   use, found 12 families, and missed 9 including every `scroll-m*`, `scroll-p*`, `pl+pr`,
 *   `ml+mr`, `translate` and `skew`. A table built from a corpus is silently wrong the day someone
 *   writes a family the corpus never contained. A table built from the registry cannot be.
 *
 *   No prefilter. Pairing only roots that share a first segment runs in 3.7s instead of 17.3s and
 *   loses `bottom + top => inset-y` and `left + right => inset-x`. That is the same
 *   over-approximation asymmetry this rule has been bitten by three times: a cheap filter costs
 *   nothing visible and removes real findings. 17.3s offline is not worth optimising.
 *
 * Emits JSON on stdout for the Go side to render. Deliberately does no formatting itself, so the
 * shape of the generated file is decided in one place.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodeModule from 'node:module';
import * as NodePath from 'node:path';
import * as NodeUrl from 'node:url';

const [, , entryPointArgument, projectRootArgument] = process.argv;
if (!entryPointArgument) {
    process.stderr.write('usage: enumerate.mjs <theme.css> [projectRoot]\n');
    process.exit(2);
}

const entryPointPath = NodePath.resolve(entryPointArgument);
const projectRoot = NodePath.resolve(projectRootArgument ?? NodePath.dirname(entryPointPath));

/*
 * Resolve Tailwind's ESM entry.
 *
 * `require.resolve` takes the `require` branch of the exports map, and for Tailwind that is
 * `dist/lib.js`, which does not export `__unstable__loadDesignSystem` at all. Only the ESM build
 * does, so the package root is located through its `package.json` and the `import` condition read
 * directly.
 */
function resolveModuleUrl(specifier, directory) {
    const requireFromDirectory = NodeModule.createRequire(NodePath.join(directory, 'noop.js'));
    const packageJsonPath = requireFromDirectory.resolve(specifier + '/package.json');
    const packageJson = JSON.parse(NodeFileSystem.readFileSync(packageJsonPath, 'utf8'));
    const rootExport = packageJson.exports?.['.'] ?? packageJson.exports;
    const importEntry = typeof rootExport === 'string' ? rootExport : (rootExport?.import ?? rootExport?.default);
    const resolvedPath = importEntry
        ? NodePath.join(NodePath.dirname(packageJsonPath), importEntry)
        : requireFromDirectory.resolve(specifier);
    return NodeUrl.pathToFileURL(resolvedPath).href;
}

function resolveStylesheet(specifier, directory) {
    if (specifier.startsWith('.') || NodePath.isAbsolute(specifier)) {
        return NodePath.resolve(directory, specifier);
    }
    const requireFromDirectory = NodeModule.createRequire(NodePath.join(directory, 'noop.js'));
    if (specifier.endsWith('.css')) return requireFromDirectory.resolve(specifier);

    const packageJsonPath = requireFromDirectory.resolve(specifier + '/package.json');
    const packageJson = JSON.parse(NodeFileSystem.readFileSync(packageJsonPath, 'utf8'));
    const rootExport = packageJson.exports?.['.'] ?? packageJson.exports;
    const styleEntry = (typeof rootExport === 'object' ? rootExport?.style : undefined) ?? packageJson.style;
    if (!styleEntry) return requireFromDirectory.resolve(specifier);
    return NodePath.join(NodePath.dirname(packageJsonPath), styleEntry);
}

const entryDirectory = NodePath.dirname(entryPointPath);
const tailwindModule = await import(resolveModuleUrl('tailwindcss', entryDirectory));

const designSystem = await tailwindModule.__unstable__loadDesignSystem(
    NodeFileSystem.readFileSync(entryPointPath, 'utf8'),
    {
        base: entryDirectory,
        loadModule: async function (specifier, base, resourceType) {
            try {
                const requireFromBase = NodeModule.createRequire(NodePath.join(base, 'noop.js'));
                const resolved = requireFromBase.resolve(specifier);
                const loaded = await import(NodeUrl.pathToFileURL(resolved).href);
                return { base: NodePath.dirname(resolved), module: loaded.default ?? loaded };
            }
            catch {
                return { base, module: resourceType === 'config' ? {} : () => {} };
            }
        },
        loadStylesheet: async function (specifier, base) {
            try {
                const resolved = resolveStylesheet(specifier, base);
                return { base: NodePath.dirname(resolved), content: NodeFileSystem.readFileSync(resolved, 'utf8') };
            }
            catch {
                return { base: '', content: '' };
            }
        },
    },
);

/*
 * `canonicalizeCandidates` is opt-in and the options are load-bearing.
 *
 * Called with no options, `['px-4','py-4']` returns its input unchanged, so a generator that omits
 * them enumerates zero families and produces an empty table that looks like a clean answer.
 *
 * `rem` is deliberately absent. Passing it rewrites arbitrary values, turning `h-[76px]` into `h-19`,
 * and produces findings upstream does not report.
 */
const canonicalizeOptions = { collapse: true, logicalToPhysical: true };

function parseCandidate(className) {
    try {
        return Array.from(designSystem.parseCandidate?.(className) ?? [])[0] ?? null;
    }
    catch {
        return null;
    }
}

// Every functional root the design system knows, discovered by asking it for its class list and
// reading the root back out of the parser rather than by splitting names on dashes. A dash-splitter
// reads `border-l` as root `border` with value `l`, which is the mistake that silently stopped
// reporting `border-x`.
const functionalRoots = new Set();
const staticUtilities = new Set();
for (const entry of designSystem.getClassList?.() ?? []) {
    const name = Array.isArray(entry) ? entry[0] : typeof entry === 'string' ? entry : entry?.name;
    if (typeof name !== 'string') continue;

    const asFunctional = parseCandidate(name + '-4');
    if (asFunctional?.kind === 'functional' && asFunctional.root) {
        functionalRoots.add(asFunctional.root);
        continue;
    }
    const asStatic = parseCandidate(name);
    if (asStatic?.kind === 'static') staticUtilities.add(name);
}

const roots = Array.from(functionalRoots).sort();

/*
 * The probe value.
 *
 * Families are value-independent, verified across `0`, `1`, `2`, `4`, `8` and `px`, so one value is
 * enough to discover a family. `4` is used because it exists in the default spacing scale for every
 * spacing-like root; a root whose scale lacks it simply produces no collapse at that value and is
 * caught by the second value below.
 */
const probeValues = ['4', '2'];

/*
 * Whether a class rewrites on its own, with no second class involved.
 *
 * Some classes are non-canonical by themselves: `start-4` is `inset-s-4`, `z-[1]` is `z-1`. Those
 * rewrites have nothing to do with pairing, and mistaking one for a family is the failure this
 * function exists to prevent. A first version of this generator counted invented class names and
 * reported 1,326 families including `-bg-conic + -end => -inset-e`, which is nonsense: `-end-4`
 * rewrites alone and `-bg-conic-4` was simply along for the ride.
 *
 * Memoized because the loop asks about each root roughly 327 times.
 */
const soloRewrites = new Map();
function rewritesAlone(className) {
    if (soloRewrites.has(className)) return soloRewrites.get(className);

    let answer = false;
    try {
        const result = designSystem.canonicalizeCandidates([className], canonicalizeOptions);
        answer = result.some((candidate) => candidate !== className);
    }
    catch {
        answer = false;
    }

    soloRewrites.set(className, answer);
    return answer;
}

/*
 * The output root two classes merge into, or null when they do not merge.
 *
 * The test is leave-one-out rather than "did a new name appear". An invented class only counts as a
 * family when it stops being produced once either input is removed, which is exactly what
 * distinguishes `px-4 + py-4 => p-4` from one class rewriting beside an unrelated neighbour.
 */
function collapseOf(rootA, rootB, value) {
    const classA = rootA + '-' + value;
    const classB = rootB + '-' + value;

    // A class that rewrites alone poisons the pair test, because its rewrite appears in the output
    // whether or not the other class had anything to do with it.
    if (rewritesAlone(classA) || rewritesAlone(classB)) return null;

    let result;
    try {
        result = designSystem.canonicalizeCandidates([classA, classB], canonicalizeOptions);
    }
    catch {
        // Tailwind throws on candidates it cannot compile. A pair that will not compile has no family.
        return null;
    }

    const inputs = [classA, classB];
    const invented = result.filter((className) => !inputs.includes(className));
    if (invented.length !== 1) return null;

    // Both inputs must be load-bearing: removing either one has to stop the collapse happening.
    // Without this, a class that rewrites in company with anything would read as a family with
    // every root it was paired against.
    for (const soleInput of inputs) {
        let alone;
        try {
            alone = designSystem.canonicalizeCandidates([soleInput], canonicalizeOptions);
        }
        catch {
            return null;
        }
        if (alone.includes(invented[0])) return null;
    }

    // Read the output's own root back through the parser, so the table stores a root rather than a
    // root-plus-the-value-we-happened-to-probe-with.
    const outputCandidate = parseCandidate(invented[0]);
    if (outputCandidate?.kind === 'functional' && outputCandidate.root) return outputCandidate.root;
    return null;
}

const families = [];
let pairsProbed = 0;

for (let indexA = 0; indexA < roots.length; indexA++) {
    for (let indexB = indexA + 1; indexB < roots.length; indexB++) {
        pairsProbed++;
        for (const value of probeValues) {
            const outputRoot = collapseOf(roots[indexA], roots[indexB], value);
            if (outputRoot === null) continue;
            families.push({ inputs: [roots[indexA], roots[indexB]], output: outputRoot, discoveredAtValue: value });
            break;
        }
    }
}

families.sort((left, right) => left.inputs.join(' ').localeCompare(right.inputs.join(' ')));

const tailwindVersion = JSON.parse(
    NodeFileSystem.readFileSync(
        NodeModule.createRequire(NodePath.join(entryDirectory, 'noop.js')).resolve('tailwindcss/package.json'),
        'utf8',
    ),
).version;

process.stdout.write(
    JSON.stringify(
        {
            tailwindVersion,
            entryPoint: NodePath.relative(projectRoot, entryPointPath),
            functionalRoots: roots.length,
            staticUtilities: staticUtilities.size,
            pairsProbed,
            families,
        },
        null,
        2,
    ) + '\n',
);
