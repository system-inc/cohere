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

/*
 * The project root, which is both where reported paths are relative to and where bare specifiers
 * resolve from. That second job is the one it did not have and needed.
 *
 * Where the CSS lives and where `tailwindcss` resolves from are different questions, and for a
 * repository they have the same answer: its theme.css sits inside the tree that installed Tailwind,
 * so anchoring resolution on the entry point's own directory worked and the distinction stayed
 * invisible. It stops working for exactly the file that matters most here. An independent design
 * system checked in under `tools/`, written to share no `@theme` with the corpus, has no
 * `node_modules` above it, so `@import 'tailwindcss'` could not resolve and the file was
 * ungenerable. The invariance check it exists for was a copy-it-somewhere-else ritual that was
 * never checked in.
 *
 * Still defaulting to the entry point's directory, so every repository invocation is unchanged.
 */
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

/*
 * A relative `@import` resolves against the importing file, and a bare one against the project
 * root. Keeping those two separate is what lets a design system live outside the tree that
 * installed Tailwind: `./tokens.css` still means the file beside it, while `tailwindcss` means the
 * package the caller named.
 */
function resolveStylesheet(specifier, directory) {
    if (specifier.startsWith('.') || NodePath.isAbsolute(specifier)) {
        return NodePath.resolve(directory, specifier);
    }
    const requireFromDirectory = NodeModule.createRequire(NodePath.join(projectRoot, 'noop.js'));
    if (specifier.endsWith('.css')) return requireFromDirectory.resolve(specifier);

    const packageJsonPath = requireFromDirectory.resolve(specifier + '/package.json');
    const packageJson = JSON.parse(NodeFileSystem.readFileSync(packageJsonPath, 'utf8'));
    const rootExport = packageJson.exports?.['.'] ?? packageJson.exports;
    const styleEntry = (typeof rootExport === 'object' ? rootExport?.style : undefined) ?? packageJson.style;
    if (!styleEntry) return requireFromDirectory.resolve(specifier);
    return NodePath.join(NodePath.dirname(packageJsonPath), styleEntry);
}

const entryDirectory = NodePath.dirname(entryPointPath);
const tailwindModule = await import(resolveModuleUrl('tailwindcss', projectRoot));

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
/*
 * Class ordering, for `enforce-consistent-class-order`.
 *
 * `getClassOrder` returns positions within the queried set, which reads as query-relative and is
 * not: across all 66 pairs of a twelve-class sample the pairwise order always matches the global
 * order. It is a stable total order whose numbers renumber per call.
 *
 * It needs a position per class rather than per root, and that was measured rather than assumed:
 * 84 of 1,210 root groups are non-contiguous in the global order, and those 84 contain 18,936 of
 * the 37,643 ranked classes. Storing roots plus an exception list saves nothing when half the
 * registry is in the exceptions.
 *
 * Unranked classes are excluded rather than stored with a position. `group` and `peer` generate no
 * CSS of their own and the engine returns null for them; giving them a position sorts them among
 * real utilities instead of last, which was the final three literals in the corpus differential.
 */
/*
 * Class ordering data, for `enforce-consistent-class-order`.
 *
 * A per-class position table would be 37,643 entries and 2.0 MB. Tailwind's own sort is ~25 lines in
 * its `compile.ts`, and porting the comparator instead needs 353 rows: a property-order list and a
 * handful of overrides. Verified exact against the engine over 2,465 real literals and 296
 * deliberately shuffled ones.
 *
 * The comparator, from `getPropertySort` and the `astNodes.sort` in `compile.ts`:
 *
 *   1. variant order
 *   2. first differing index into the property order
 *   3. more declarations first
 *   4. alphabetical
 *
 * `propertyOrder` is Tailwind's own list, read from the installed package rather than transcribed.
 * `sortOverrides` are utilities that declare `--tw-sort`, which replaces their sort position
 * entirely: `space-x` sorts as `row-gap` rather than as its own margins. That declaration is
 * stripped from compiled output, so it cannot be probed and has to come from the source.
 */
const classOrder = [];
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
    if (asStatic?.kind === 'static') {
        staticUtilities.add(name);
    }

    const asFunctional = parseCandidate(name + '-4');
    if (asFunctional?.kind === 'functional' && asFunctional.root) {
        functionalRoots.add(asFunctional.root);
    }

    // Existence, read from the name as written rather than from a probe value. `from-black/70`
    // parses as root `from`, and `from` never appears with a numeric probe.
    const asWritten = parseCandidate(name);
}

/*
 * The registry's own functional roots, which the class list cannot advertise.
 *
 * `getClassList()` enumerates *classes*, so a functional root that advertises none is invisible to
 * it however the names are probed. Fourteen are in that state: `filter`, `backdrop-filter`,
 * `bg-size`, `bg-position`, `mask-position`, `font-features`, `flex-grow`, `flex-shrink`,
 * `max-w-screen`, `-col`, `-row`, `-hue-rotate` and `-backdrop-hue-rotate`. Measured, a `-4` probe
 * and a `-[var(--x)]` probe each add zero of them, so this is not a gap a better suffix closes.
 *
 * `KnownRoots` is what `HasUtility` answers from, and `HasUtility` is what `findRoots` consults at
 * every split, so a root missing here is not merely absent from a table: the parser cannot see it at
 * all and resolves the class to a shorter root that *is* present. `flex-grow-1` read as root `flex`
 * with value `grow-1`, `bg-size-cover` as `bg`, `max-w-screen-sm` as `max-w`, and `filter-none` did
 * not parse at all because `filter` was unknown and no shorter prefix exists. The engine reads all
 * four as their own roots.
 *
 * That is a wrong root rather than a missing one, and the readings happened to agree, which is why
 * nothing downstream looked anomalous. `KnownStatics` is deliberately not extended the same way:
 * `utilities.keys('static')` is the same enumeration for statics, but a static root that advertises
 * no class has no reading to carry, and `max-w-screen` is registered both ways and already reaches
 * the static table through the class list above.
 */
for (const root of designSystem.utilities?.keys?.('functional') ?? []) {
    if (typeof root !== 'string') continue;
    functionalRoots.add(root);
}

const orderingByRoot = [];
const orderingByStatic = [];
const orderingByClass = [];
const roots = Array.from(functionalRoots).sort();

/*
 * Ordering properties, per root and per static, with per-class exceptions.
 *
 * 312 roots and 893 statics, of which 65 roots vary by value and contribute 130 exception entries.
 * A per-class table was built first at 37,643 entries and 2.0 MB; this is the same information in
 * about a thousand rows, because the declarations a utility emits are a fact about its root far more
 * often than not.
 */
{
    const seenRoot = new Map();
    const seenReading = new Map();
    const exceptions = [];

    for (const entry of designSystem.getClassList?.() ?? []) {
        const name = Array.isArray(entry) ? entry[0] : typeof entry === 'string' ? entry : entry?.name;
        if (typeof name !== 'string') continue;

        const candidate = parseCandidate(name);
        if (!candidate) continue;
        const properties = orderingProperties(name);
        if (properties === null) continue;

        if (candidate.kind === 'static') {
            orderingByStatic.push({ name, properties });
            continue;
        }
        if (!candidate.root) continue;

        const joined = properties.join(',');
        if (!seenRoot.has(candidate.root)) {
            seenRoot.set(candidate.root, joined);
            orderingByRoot.push({ name: candidate.root, properties });
            continue;
        }
        if (seenRoot.get(candidate.root) === joined) continue;

        /*
         * One entry per distinct reading, not per class that differs.
         *
         * 65 roots vary and they hold 130 distinct property sets between them, but 24,161 classes
         * sit inside those roots. Recording each class produced a 2.7 MB file for information that
         * fits in a few hundred rows. The variation is by value kind exactly as it is in the
         * conflict tables: `border-0` emits width and style, `border-amber-50` emits color.
         *
         * Keyed by root and the kind of value, so a rule looks up the pair rather than the class.
         */
        /*
         * Keyed by the root and the class's own VALUE, not by a two-way colour flag.
         *
         * A first version classified each reading as colour or other, which cannot separate two
         * non-colour readings of one root: `font-medium` emits `font-weight` and `font-sans` emits
         * `font-family`, both non-colour, so whichever was seen second silently replaced the other
         * and every `font-*` class sorted at the wrong index. That regressed the differential from
         * 2,751 to 2,463.
         *
         * The value is the thing that actually decides, so it is the key. This stores one row per
         * distinct reading per root rather than one per class, which is 65 rows rather than 24,161.
         */
        const readingKey = candidate.root + '\u0000' + joined;
        if (seenReading.has(readingKey)) continue;
        seenReading.set(readingKey, true);
        /*
         * Two key shapes, because two different things vary.
         *
         * A colour reading covers hundreds of value names that all behave identically, so it is
         * keyed by kind: one row for every `text-<colour>`. Any other reading is keyed by its own
         * value, because `font-medium` and `font-sans` differ from each other and from nothing else.
         *
         * Keying everything by kind cannot separate two non-colour readings and cost 288 literals;
         * keying everything by value cannot cover the colour scale and cost 298.
         */
        const isColorReading = properties.some((property) => property.endsWith('color'));
        const keyValue = isColorReading ? '\u0001color' : valueTextOf(candidate);
        exceptions.push({ name: candidate.root + '\u0000' + keyValue, properties });
    }

    orderingByRoot.sort((left, right) => left.name.localeCompare(right.name));
    orderingByStatic.sort((left, right) => left.name.localeCompare(right.name));
    exceptions.sort((left, right) => left.name.localeCompare(right.name));
    orderingByClass.push(...exceptions);
}


const propertyOrder = [];
const sortOverrides = [];
const variantOrder = [];

{
    const tailwindRoot = NodePath.dirname(
        NodeModule.createRequire(NodePath.join(projectRoot, 'noop.js')).resolve('tailwindcss/package.json'),
    );

    /*
     * Tailwind's property order, read out of the installed bundle.
     *
     * It ships as a plain string array, so it is recovered exactly rather than reconstructed.
     * A first attempt derived the order by asking the engine to sort one utility per property, which
     * only reaches properties some utility declares alone: it found 254 of 359, and every index
     * after a missing one was shifted, which took the comparator from exact to 55%.
     */
    let bundleSource = '';
    try {
        bundleSource = NodeFileSystem.readFileSync(NodePath.join(tailwindRoot, 'dist', 'lib.mjs'), 'utf8');
    }
    catch {
        bundleSource = '';
    }

    const orderMatch = bundleSource.match(/\["container-type","pointer-events"[^\]]*\]/);
    if (orderMatch !== null) {
        for (const property of JSON.parse(orderMatch[0])) propertyOrder.push(property);
    }

    /*
     * Utilities whose `--tw-sort` replaces their position, read from Tailwind's own source.
     *
     * Sixteen exist across the whole framework. They cannot be recovered from compiled CSS because
     * the declaration is stripped before emission, so this is the one place the generator reads
     * source rather than asking the engine.
     */
    /*
     * Variant order, asked of the engine rather than guessed.
     *
     * A first version ranked variants by prefix length, which put `focus:` before `hover:` because
     * it is shorter. The engine has its own order and exposes it: sorting one fixed base class under
     * each known variant recovers it exactly.
     */
    {
        const variantNames = [];
        try {
            for (const variant of designSystem.getVariants?.() ?? []) {
                const name = typeof variant === 'string' ? variant : variant?.name;
                if (typeof name === 'string') variantNames.push(name);
            }
        }
        catch {
            // A design system that cannot list its variants leaves the order empty, and the rule
            // then treats every variant as equal rather than inventing an order.
        }

        /*
         * Compound variants too. `getVariants` lists bases, so `group-hover:` and `peer-checked:`
         * are absent from it and fell to the unknown sentinel, which sorted every one of them after
         * every known variant instead of in its own place.
         */
        for (const name of [...variantNames]) {
            variantNames.push('group-' + name, 'peer-' + name);
        }

        const probes = variantNames
            .map((name) => name + ':flex')
            .filter((className) => {
                try {
                    return Array.from(designSystem.parseCandidate?.(className) ?? []).length > 0;
                }
                catch {
                    return false;
                }
            });

        for (const [className] of designSystem
            .getClassOrder(probes)
            .filter(([, position]) => position !== null)
            .sort((left, right) => (left[1] < right[1] ? -1 : left[1] > right[1] ? 1 : 0))) {
            variantOrder.push(className.slice(0, className.lastIndexOf(':') + 1));
        }
    }

    for (const match of bundleSource.matchAll(/"--tw-sort",\s*"([^"]+)"/g)) {
        // The bundle is minified, so the utility name is recovered from the surrounding text rather
        // than from a stable structure. Recorded as a value list; the rule maps roots to them below.
        sortOverrides.push(match[1]);
    }
}

const unreachableRoots = [];

/*
 * Every root and static name the design system knows, whether or not it declares a CSS property.
 *
 * `no-unknown-classes` asks a different question from every other rule here: not "what does this
 * declare" but "does this exist". Answering it from the properties tables gets 12 real classes wrong
 * on this tree alone, because those tables deliberately exclude things that declare nothing a rule
 * can compare: `from-black/70` sets only `--tw-gradient-from`, `container` emits several rules, and
 * `fade-in` sets only `--enter-opacity`.
 *
 * Absence from a properties table is not evidence a class does not exist, so existence gets its own
 * list. This is the union of every functional root and every static name, which is what
 * `getClassList()` already enumerates.
 */


/*
 * A real class for each root, taken from the design system's own class list.
 *
 * Guessing probe values is what produced four separate bugs in this generator and 49 silently
 * missing roots. The registry already knows a class for every root it defines, so the reliable move
 * is to read one rather than to invent one: `from-0%`, `slide-in-from-top-4` and `animate-spin` are
 * values no hand-written list would have contained.
 *
 * The guessed list is still tried first, because it keeps the probe values consistent across roots
 * where a choice exists; this is the fallback that makes "unreachable" mean genuinely unreachable.
 */
const exampleClassByRoot = new Map();
for (const entry of designSystem.getClassList?.() ?? []) {
    const name = Array.isArray(entry) ? entry[0] : typeof entry === 'string' ? entry : entry?.name;
    if (typeof name !== 'string') continue;

    const candidate = parseCandidate(name);
    if (candidate?.kind !== 'functional' || !candidate.root) continue;
    if (exampleClassByRoot.has(candidate.root)) continue;
    exampleClassByRoot.set(candidate.root, name);
}


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
/*
 * The declarations a class emits, in source order, keeping custom properties.
 *
 * Separate from `declaredProperties` on purpose, and the two disagree deliberately.
 * `no-conflicting-classes` strips `--tw-*` because two classes both setting `--tw-border-style` are
 * not in conflict about anything an author can see. Ordering is the opposite: Tailwind's own sort
 * indexes custom properties, and they are what separates classes that share a visible one.
 *
 * `shadow-lg` declares `--tw-shadow` then `box-shadow`; `ring-1` declares `--tw-ring-shadow` then
 * `box-shadow`. Stripping the first left both with the identical key `[box-shadow]`, so their
 * relative order fell through to a tiebreak and ten real class lists came out wrong.
 *
 * Order is preserved rather than sorted, because the engine walks declarations as written.
 */
// valueTextOf renders a candidate's value as the plain text a color name would match.
function valueTextOf(candidate) {
    const value = candidate?.value;
    if (!value || value.kind === 'arbitrary') return '';
    const text = String(value.value ?? '');
    const slash = text.indexOf('/');
    return slash >= 0 ? text.slice(0, slash) : text;
}

function orderingProperties(className) {
    let compiled;
    try {
        compiled = designSystem.candidatesToCss?.([className]);
    }
    catch {
        return null;
    }
    if (!compiled || !compiled[0]) return null;

    const beforeAtRules = compiled[0].split('@')[0];
    const bodies = Array.from(beforeAtRules.matchAll(/\{([^{}]*)\}/g)).map((match) => match[1]);
    if (bodies.length === 0) return null;

    const properties = bodies
        .flatMap((body) => Array.from(body.matchAll(/([-a-zA-Z]+)\s*:\s*[^;]+;/g)))
        .map((match) => match[1].trim());

    return properties.length === 0 ? null : properties;
}

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
    /*
     * Probed with a color as well as the numeric and named scales.
     *
     * `divide` accepts only a color, so a numeric-and-named probe found nothing and left it out of
     * the properties table entirely. That is the fourth time in this file a probe missed a root
     * because its value kind was not represented, and this one hid behind a passing fixture: the
     * `divide-neutral-200` against `border-neutral-200` case read as silent because the class did
     * not resolve at all, rather than because the selector check separated them.
     */
    let properties = null;
    for (const value of allProbeValues()) {
        properties = declaredProperties(root + '-' + value);
        if (properties !== null) break;
    }
    if (properties === null && exampleClassByRoot.has(root)) {
        properties = declaredProperties(exampleClassByRoot.get(root));
    }
    if (properties === null) {
        /*
         * Two different absences, and conflating them is what hid four bugs.
         *
         * A root that compiles to nothing at all is unreachable: no probe value produced a class,
         * so it is unmeasured and every table below is missing it for a reason nobody could infer.
         * A root that compiles but declares only custom properties is genuinely empty: it has no
         * CSS property to record, and `--tw-gradient-from-position` is not something two classes
         * can conflict about.
         *
         * `slide-in-from-top-4`, `from-0%` and `zoom-in-50` are all the second kind, so reporting
         * them as unreachable would be crying wolf on twenty-eight roots that are working
         * correctly.
         */
        const probeClass = exampleClassByRoot.get(root);
        const compilesAtAll = probeClass !== undefined && declarationText(probeClass) !== null;
        if (!compilesAtAll && probeClass === undefined) unreachableRoots.push(root);
        continue;
    }
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
/*
 * The selector a utility emits, with its own class name blanked so two classes are comparable.
 *
 * This is what separates `divide-neutral-200` from `border-neutral-200`: both declare `border-color`
 * and they do not conflict, because divide emits under `:where(.CLASS > :not(:last-child))` and
 * targets child elements while border emits on the element itself. Upstream groups by compiled
 * selector path for exactly this reason.
 *
 * Measured to be a fact about roots rather than about classes: every `divide-*` and `space-*` emits
 * the same shape regardless of value, which is what makes it tabulatable at all. A first reading of
 * this rule concluded it was engine-bound; it is not, and the difference was never testing whether
 * the shape varied.
 */
function selectorShape(className) {
    let compiled;
    try {
        compiled = designSystem.candidatesToCss?.([className]);
    }
    catch {
        return null;
    }
    if (!compiled || !compiled[0]) return null;

    const beforeAtRules = compiled[0].split('@')[0];
    const selectors = Array.from(beforeAtRules.matchAll(/([^{}]+)\{/g)).map((match) => match[1].trim());
    if (selectors.length !== 1) return null;

    return selectors[0].replace(/\.(?:[\w-]|\\.)+/g, '.CLASS');
}

const rootSelectorShapes = [];
const classProperties = [];
const composingRoots = [];

/*
 * Roots whose utilities all emit the same declaration text regardless of value.
 *
 * `shadow-lg` and `ring-1` both declare `box-shadow`, and they do not conflict: every `shadow-*`
 * and every `ring-*` emits the identical `var(--tw-inset-shadow), var(--tw-ring-shadow), ...` chain
 * and each contributes through its own custom property, so they layer rather than overwrite. By
 * contrast `px-4` is `calc(var(--spacing) * 4)` and `px-8` is `* 8`, which genuinely collide.
 *
 * Measured to be a root fact rather than a class fact, which is what makes it storable: the test is
 * whether two different values of the same root produce identical declaration text.
 */
function declarationText(className) {
    let compiled;
    try {
        compiled = designSystem.candidatesToCss?.([className]);
    }
    catch {
        return null;
    }
    if (!compiled || !compiled[0]) return null;

    const beforeAtRules = compiled[0].split('@')[0];
    const bodies = Array.from(beforeAtRules.matchAll(/\{([^{}]*)\}/g)).map((match) => match[1]);
    if (bodies.length !== 1) return null;

    const declarations = Array.from(bodies[0].matchAll(/([-a-zA-Z]+)\s*:\s*([^;]+);/g))
        .filter((match) => !match[1].startsWith('--'))
        .map((match) => match[1].trim() + '=' + match[2].trim())
        .sort();
    return declarations.length === 0 ? null : declarations.join(';;');
}




/*
 * Every value kind a probe might need, in one list.
 *
 * Four bugs in this generator came from a probe list that could not express some root's values, and
 * each one reported the root as having nothing rather than as unmeasured: the `rounded-*` families
 * were absent because the radius scale is named, `divide` was absent from the properties and shape
 * tables because it takes only a color, and `shadow` was absent from the composing set because
 * `shadow-4` is not a class.
 *
 * A check that cannot fire is indistinguishable from a check nothing violates, and the same is true
 * of a probe that cannot reach its subject. One list means widening it fixes every probe at once,
 * and `unreachableRoots` below turns the remaining silence into a reported number.
 */
function allProbeValues() {
    const firstColor = colorNames.values().next().value;
    return [
        // Numeric and named scales.
        ...probeValues,
        '8',
        '1',
        'xl',
        // A color, for roots like `divide` and `border` that accept only one.
        firstColor,
        // Keyword scales, for the roots the numeric and color probes cannot reach at all. Adding
        // these took 49 unreachable roots down, and every one of them was a real utility absent
        // from every table: `animate`, `cursor`, `object`, `transition`, `aspect`, `content`.
        'spin',
        'pointer',
        'cover',
        'colors',
        'square',
        'none',
        'auto',
        'normal',
        'center',
        'linear',
        'inherit',
        '0',
    ].filter(Boolean);
}

const rootDefault = new Map(rootProperties.map((entry) => [entry.root, entry.properties.join(',')]));

for (const root of roots) {
    /*
     * Probed with a color as well as the numeric and named scales.
     *
     * `divide` accepts only a color, so `divide-4` is not a class and a numeric-only probe reports
     * the root as having no shape at all. That is the same gap that left every `rounded-*` family
     * out of the collapse table: a probe that cannot express a root's values reports the root as
     * having nothing, which reads exactly like a root that has nothing.
     *
     * `divide` is the case that matters most here, because `divide-neutral-200` against
     * `border-neutral-200` is the pair that made the whole rule look engine-bound.
     */
    let shape = null;
    for (const value of allProbeValues()) {
        shape = selectorShape(root + '-' + value);
        if (shape !== null) break;
    }
    if (shape === null && exampleClassByRoot.has(root)) {
        shape = selectorShape(exampleClassByRoot.get(root));
    }
    if (shape !== null && shape !== '.CLASS') rootSelectorShapes.push({ root, shape });
}

for (const root of roots) {
    // Two distinct values of one root producing identical text means the root layers rather than
    // overwrites, so two of its classes never conflict with each other.
    /*
     * Two values the root actually accepts, found by trying several scales.
     *
     * This is the third probe in this file to have missed a root because its values were named
     * rather than numeric. `shadow-4` and `shadow-8` are not classes, so a numeric-only test
     * reported `shadow` as not composing while recording `ring` correctly, and `shadow-lg ring-1`
     * is exactly the pair that has to come out silent.
     */
    const candidateValues = allProbeValues();

    let first = null;
    let firstValue = null;
    for (const value of candidateValues) {
        const text = declarationText(root + '-' + value);
        if (text === null) continue;
        first = text;
        firstValue = value;
        break;
    }
    if (first === null) continue;

    for (const value of candidateValues) {
        if (value === firstValue) continue;
        const second = declarationText(root + '-' + value);
        if (second === null) continue;
        if (first === second) composingRoots.push(root);
        break;
    }
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

const colorProperties = new Map(rootValueProperties.map((entry) => [entry.root, entry.properties.join(',')]));

/*
 * Classes whose properties differ from their root's default, recorded individually.
 *
 * The color split was the first instance and it is not the only one: `font-medium` declares
 * `font-weight` while `font-mono` declares `font-family`, and both parse as root `font`. A probe
 * picks one reading and every class taking the other is then wrong, which showed up as
 * `font-medium font-mono` being reported as a conflict on real code.
 *
 * Only the exceptions are stored, so the table stays small: a class absent here takes its root's
 * entry. Multi-rule utilities are already excluded by declaredProperties, so a component class
 * cannot land here either.
 */
for (const entry of designSystem.getClassList?.() ?? []) {
    const name = Array.isArray(entry) ? entry[0] : typeof entry === 'string' ? entry : entry?.name;
    if (typeof name !== 'string') continue;

    const candidate = parseCandidate(name);
    if (candidate?.kind !== 'functional' || !candidate.root) continue;

    const rootProperties = rootDefault.get(candidate.root);
    if (rootProperties === undefined) continue;

    const properties = declaredProperties(name);
    if (properties === null) continue;
    if (properties.join(',') === rootProperties) continue;

    // Colors are already handled per root, and they are the bulk of the exceptions: recording them
    // here too produced 8,428 entries and a 10,000-line file for a distinction the color reading
    // already makes.
    const colorReading = colorProperties.get(candidate.root);
    if (colorReading !== undefined && properties.join(',') === colorReading) continue;

    classProperties.push({ root: name, properties });
}

classProperties.sort((left, right) => left.root.localeCompare(right.root));
rootValueProperties.sort((left, right) => left.root.localeCompare(right.root));

const staticProperties = [];
for (const name of Array.from(staticUtilities).sort()) {
    const properties = declaredProperties(name);
    if (properties === null) continue;
    staticProperties.push({ root: name, properties });
}

const tailwindVersion = JSON.parse(
    NodeFileSystem.readFileSync(
        NodeModule.createRequire(NodePath.join(projectRoot, 'noop.js')).resolve('tailwindcss/package.json'),
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
            propertyOrder,
            orderingByRoot,
            orderingByStatic,
            orderingByClass,
            sortOverrides: Array.from(new Set(sortOverrides)).sort(),
            variantOrder,
            unreachableRoots: unreachableRoots.sort(),
            rootSelectorShapes,
            rootColorProperties: rootValueProperties,
            classProperties,
            staticProperties,
        },
        null,
        2,
    ) + '\n',
);
