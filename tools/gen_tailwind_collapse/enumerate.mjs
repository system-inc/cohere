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

    // A name can be BOTH, and choosing one loses the other. `flex` is the static utility
    // `display: flex` and also the functional root of `flex-4`, which is `flex: 4`; `block` is
    // `display: block` and the root of `block-4`, which is `block-size`. A first version checked
    // functional first and returned early, so `flex` and `block` were recorded with the properties
    // of their functional readings and vanished from the static table entirely. That would have
    // silently stopped `flex block` being reported as a display conflict, which is the headline
    // case of the rule this data feeds.
    const asStatic = parseCandidate(name);
    if (asStatic?.kind === 'static') staticUtilities.add(name);

    const asFunctional = parseCandidate(name + '-4');
    if (asFunctional?.kind === 'functional' && asFunctional.root) {
        functionalRoots.add(asFunctional.root);
    }
}

const roots = Array.from(functionalRoots).sort();

/*
 * The probe values.
 *
 * Families are value-independent, verified across `0`, `1`, `2`, `4`, `8` and `px`, so one value a
 * root accepts is enough to discover its family. The catch is that not every root accepts the same
 * kind of value, and a probe list that misses a root's scale silently omits its families.
 *
 * `4` and `2` alone left out every `rounded-*` family, because the radius scale is named (`sm`,
 * `md`, `lg`) rather than numeric. That was found by diffing the table against the engine over the
 * real corpus, where `rounded-tl-md + rounded-tr-md => rounded-t-md` is a genuine finding the table
 * could not reach. A generator whose probe cannot express a root's values reports that root as
 * having no families, which reads exactly like a root that has none.
 */
const probeValues = ['4', '2', 'md', 'sm', 'lg', 'full', 'px'];

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

/*
 * The CSS properties each root declares, for `no-conflicting-classes`.
 *
 * Two classes conflict when they declare the same property, so the rule needs a property set per
 * class. Measured across `1`, `4`, `8`, `left`, `right` and `center`: every root produces one
 * distinct property set regardless of value, so this is a fact about roots exactly like the collapse
 * families are, and the table stays small rather than growing with the theme's scale.
 *
 * Property NAMES, not values. `w-8` and `h-8` declare the same value under `width` and `height` and
 * do not conflict; `px-4` and `px-8` declare different values under one property and do. Keying on
 * values would invert both answers.
 *
 * `p` and `px` are deliberately allowed to differ: `padding` and `padding-inline` are different
 * property names, and upstream reports no conflict between `p-4` and `px-8` even though they
 * visually overlap. Normalising shorthands here would invent a finding upstream does not report.
 */
function declaredProperties(className) {
    let compiled;
    try {
        compiled = designSystem.candidatesToCss?.([className]);
    }
    catch {
        return null;
    }
    if (!compiled || !compiled[0]) return null;

    /*
     * Only the utility's own rule body, stopping at the first at-rule.
     *
     * `border-l-4` compiles to its two declarations followed by an `@property --tw-border-style`
     * block, and that block's descriptors are `syntax`, `inherits` and `initial-value`. Scanning
     * the whole output picks those up as if the class declared them, so every class that touches
     * border style would appear to share three properties with every other one and report a
     * conflict. The at-rule is Tailwind's plumbing rather than anything the author wrote.
     *
     * Custom properties are dropped for the same reason: two classes both setting `--tw-border-style`
     * are not in conflict about anything the author can see.
     */
    /*
     * Only declarations inside a rule body, and only from a single-rule utility.
     *
     * A first version scanned `compiled[0].split('@')[0]` with a `name: value;` regex, which reads
     * the CSS as though it were one flat block. Two things break that, and a sweep of all 1,145
     * table entries against the engine found both:
     *
     *   `.slide-in-from-end:dir(ltr) { --enter-translate-x: 100%; }` declares only a custom
     *   property, and the regex matched the SELECTOR `slide-in-from-end` from the `:dir(ltr)` part
     *   as if it were a property name. So a class declaring nothing was recorded as declaring
     *   itself, and any two such classes would read as conflicting.
     *
     *   `.markdown-content p { ... } .markdown-content code { ... }` is a component class emitting
     *   several rules, and the regex harvested `p`, `code` and `pre` as property names.
     *
     * Braces are the fix: a declaration is inside them and a selector is not. A utility emitting
     * more than one rule is skipped entirely rather than merged, because the properties it declares
     * belong to descendants rather than to the element the class sits on, and a conflict rule
     * comparing them against a sibling's would be comparing different elements.
     */
    // At-rule blocks first, then braces. Both guards are needed and each was added after the other
    // was already there: dropping the at-rule split reintroduced `syntax`, `inherits` and
    // `initial-value` from `@property`, because braces match inside at-rules too.
    const beforeAtRules = compiled[0].split('@')[0];
    const bodies = Array.from(beforeAtRules.matchAll(/\{([^{}]*)\}/g)).map((match) => match[1]);
    if (bodies.length === 0) return null;

    /*
     * A utility emitting several rules is skipped rather than merged.
     *
     * `markdown-content`, `prose` and `typing-dots` are component classes whose rules target
     * descendants: `.markdown-content p`, `.markdown-content code`, `.markdown-content pre`. Their
     * declarations belong to those children rather than to the element the class sits on, so a
     * conflict rule comparing them against a sibling class would be comparing different elements.
     * Recording nothing is the honest answer, and it makes the rule silent about them rather than
     * wrong about them.
     */
    if (bodies.length > 1) return null;

    const properties = bodies
        .flatMap((body) => Array.from(body.matchAll(/([-a-zA-Z]+)\s*:\s*[^;]+;/g)))
        .map((match) => match[1].trim())
        .filter((property) => !property.startsWith('--'));

    const distinct = Array.from(new Set(properties)).sort();
    return distinct.length === 0 ? null : distinct;
}

/*
 * Properties per root, split by value when the root needs it.
 *
 * A first version recorded one property set per root, having probed only numeric and keyword values.
 * That is wrong for 23 of 294 roots, and wrong in the direction that reports false conflicts on
 * correct code: `border` is `border-width` plus `border-style` with a number and `border-color` with
 * a color, so `border border-neutral-200`, which is idiomatic Tailwind, read as a conflict. It
 * produced 347 findings on a tree whose real count is zero.
 *
 * The split is by value kind rather than by exact value, verified across ten colors, six numbers and
 * three arbitrary values: every color-valued border gives `border-color`, every numeric one gives
 * width plus style, and `border-[#fff]` versus `border-[3px]` splits the same way. But deciding
 * which kind a value is requires knowing the theme's color names, which is precisely the
 * theme-dependent knowledge a Go-side table exists to carry. So the generator records the property
 * set per (root, value) for the values the design system actually defines, and the rule looks up the
 * pair rather than inferring the kind.
 */
const rootProperties = [];
const rootValueProperties = [];

for (const root of roots) {
    let properties = null;
    for (const value of probeValues) {
        properties = declaredProperties(root + '-' + value);
        if (properties !== null) break;
    }
    if (properties === null) continue;
    rootProperties.push({ root, properties });
}

/*
 * Roots whose properties depend on whether the value is a color, and the color names themselves.
 *
 * Recording every exception class produced 7,854 entries and a 9,000-line generated file, because
 * the exceptions are the color scale multiplied by the roots that accept it: 434 roots by 18 shades,
 * resolving to just 14 distinct property sets. That is a table nobody can read for a distinction
 * affecting 23 roots.
 *
 * The real shape is two small lists. A root that accepts both a length and a color gets a second
 * property set for its color reading, and the theme's color names are recorded once so the rule can
 * tell which reading a class is. Deciding that in Go without this list would mean hardcoding
 * Tailwind's palette, which is exactly the theme-dependent knowledge this table exists to carry.
 */
const rootDefault = new Map(rootProperties.map((entry) => [entry.root, entry.properties.join(',')]));

const colorNames = new Set();
for (const entry of designSystem.getClassList?.() ?? []) {
    const name = Array.isArray(entry) ? entry[0] : typeof entry === 'string' ? entry : entry?.name;
    if (typeof name !== 'string') continue;
    if (!name.startsWith('text-')) continue;

    const candidate = parseCandidate(name);
    if (candidate?.kind !== 'functional' || candidate.root !== 'text') continue;

    const properties = declaredProperties(name);
    if (properties === null || properties.join(',') !== 'color') continue;

    const value = candidate.value?.value;
    if (typeof value === 'string' && value !== '') colorNames.add(value);
}

for (const root of roots) {
    const defaultProperties = rootDefault.get(root);
    if (defaultProperties === undefined) continue;

    // Probe the root with a color the theme defines. A root that gives a different answer for a
    // color than for a number needs both readings recorded.
    const sampleColor = colorNames.values().next().value;
    if (sampleColor === undefined) break;

    const colorProperties = declaredProperties(root + '-' + sampleColor);
    if (colorProperties === null) continue;
    if (colorProperties.join(',') === defaultProperties) continue;

    rootValueProperties.push({ root, properties: colorProperties });
}

rootValueProperties.sort((left, right) => left.root.localeCompare(right.root));

const staticProperties = [];
for (const name of Array.from(staticUtilities).sort()) {
    const properties = declaredProperties(name);
    if (properties === null) continue;
    staticProperties.push({ root: name, properties });
}

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
            rootProperties,
            rootColorProperties: rootValueProperties,
            colorNames: Array.from(colorNames).sort(),
            staticProperties,
        },
        null,
        2,
    ) + '\n',
);
