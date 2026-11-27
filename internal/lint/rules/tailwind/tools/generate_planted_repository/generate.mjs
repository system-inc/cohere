/*
 * Write a repository's source tree from its theme, so the corpus measurements read class lists written in
 * cohere rather than harvested from another repository (#pg78xtp).
 *
 * The tailwind tests once measured their rules over about 2,400 class lists harvested from two consumer
 * repositories. cohere's tests read only cohere (#5wj23dd), so the population is generated instead: a theme
 * checked in under collapse/testdata, and beside it a tree of `.tsx` files whose `className` lists
 * tools/generate_variant harvests exactly as it harvested a real tree. Every list is sampled from the loaded
 * design system, never from a private tree, and every class in it is asked of Tailwind before it is written.
 *
 * # What a list is made of
 *
 * A real corpus leans on its own theme, so about 40 percent of the base classes are the theme's own: half of
 * those from its `@utility` roots, half from framework roots built on its own `@theme` keys (`bg-moss`,
 * `p-gutter`). The rest are the framework's. Classes are drawn root first, then a class under the root, so
 * `tint-*` with three hundred colour classes does not crowd out `card-frame` with one.
 *
 * About half the classes carry no variant and the rest one, two or three stacked, drawn from the theme's own
 * variants (new names, overridden ones such as `dark`, breakpoints it adds), the framework's static and
 * functional ones (`group-hover`, `aria-expanded`, `max-md`), and arbitrary ones (`data-[state=open]`,
 * `min-[40rem]`, `has-[>img]`, `[&>*]`). Some classes take an arbitrary value, a negative, a colour modifier or
 * Tailwind 4's trailing `!`. Within a list no two classes set the same property under the same variants, the
 * way a list someone wrote does not, so a conflict finding over the corpus is a rule's mistake.
 *
 * # Dead classes, planted and counted
 *
 * A real corpus holds classes Tailwind compiles to nothing, and no-unknown-classes is measured on them. So a
 * few are planted on purpose: a value no `@theme` key answers on four of the theme's own functional roots
 * (`wash--4` on the planted theme), each written once bare and once under a variant, each confirmed dead by
 * `candidatesToCss`. `planted.json` beside the tree records them, so the test asserts the rule reports exactly
 * these: a rule that reports nothing fails, and so does one that reports everything.
 *
 * Collapses are planted the same way for enforce-canonical-classes: four pairs Tailwind's canonicalizer merges
 * into one class (`mt-gutter mb-gutter` to `my-gutter`), each in a list of its own and recorded with the class
 * it becomes, and every other list is one the canonicalizer merges nothing in.
 *
 * # Reproducible
 *
 * A fixed-seed generator over the design system's own lists, written to stable file names, so the same theme
 * and Tailwind write the same tree. At most 1,200 distinct lists of two to nine distinct classes, which is
 * what tools/generate_variant keeps, so the fixture harvests every list and the dead classes with them.
 * `--check` writes the tree into a temporary directory and fails, naming each file, when the committed tree
 * differs, so a stale tree is visible.
 *
 * Usage, from the repository root:
 *
 *   node internal/lint/rules/tailwind/tools/generate_planted_repository/generate.mjs <theme.css> <source dir> \
 *       [--resolve-root <dir holding node_modules/tailwindcss>] [--check]
 */

import * as NodeFileSystem from 'node:fs';
import * as NodeOperatingSystem from 'node:os';
import * as NodePath from 'node:path';
import { locate } from '../corpus.mjs';
import { loadDesignSystem } from '../generate_descriptors/loader.mjs';

const usage = 'usage: generate.mjs <theme.css> <source dir> [--resolve-root <dir>] [--check]\n';
const positionalArguments = [];
let resolveRootArgument;
let check = false;
for(let index = 2; index < process.argv.length; index++) {
    const argument = process.argv[index];
    if(argument === '--resolve-root') {
        resolveRootArgument = process.argv[++index];
    }
    else if(argument === '--check') {
        check = true;
    }
    else {
        positionalArguments.push(argument);
    }
}
const [entryPointArgument, outputArgument] = positionalArguments;
if(!entryPointArgument || !outputArgument || positionalArguments.length !== 2) {
    process.stderr.write(usage);
    process.exit(2);
}

const listCount = 1200;
const deadRootCount = 4;
const collapseCount = 4;
const manifestName = 'planted.json';
const listsPerFile = 40;

/*
 * mulberry32: small, fast and the same on every machine, which is all a corpus needs. The seed is fixed so
 * the tree is a function of the theme and the Tailwind version alone.
 */
function makeRandom(seed) {
    let state = seed >>> 0;
    return function() {
        state = (state + 0x6d2b79f5) >>> 0;
        let mixed = state;
        mixed = Math.imul(mixed ^ (mixed >>> 15), mixed | 1);
        mixed ^= mixed + Math.imul(mixed ^ (mixed >>> 7), mixed | 61);
        return ((mixed ^ (mixed >>> 14)) >>> 0) / 4294967296;
    };
}
const random = makeRandom(20261125);
function pick(items) {
    return items[Math.floor(random() * items.length)];
}
function between(low, high) {
    return low + Math.floor(random() * (high - low + 1));
}
function weighted(weights) {
    const total = weights.reduce((sum, [, weight]) => sum + weight, 0);
    let roll = random() * total;
    for(const [value, weight] of weights) {
        roll -= weight;
        if(roll < 0) return value;
    }
    return weights[weights.length - 1][0];
}

const { designSystem, tailwindVersion, recordedEntryPoint, resolveRoot } = await loadDesignSystem(
    entryPointArgument,
    resolveRootArgument,
);
// The bare framework through the same install, so "the theme's own" is a difference within one engine.
const { designSystem: bareSystem } = await loadDesignSystem(
    NodePath.join(resolveRoot, '__bare_tailwind_probe__.css'),
    resolveRoot,
    '@import "tailwindcss";',
);

// What classAttributePattern in tools/generate_variant/main.go refuses inside the quotes, and quotes besides.
const unharvestable = /[\s"'`{}$\\]/;

function compiles(className) {
    if(unharvestable.test(className)) return false;
    try {
        return designSystem.candidatesToCss([className])[0] !== null;
    }
    catch {
        return false;
    }
}
function parsesButCompilesNothing(className) {
    if(unharvestable.test(className)) return false;
    try {
        return (
            designSystem.parseCandidate(className).length > 0 && designSystem.candidatesToCss([className])[0] === null
        );
    }
    catch {
        return false;
    }
}
function rootOf(system, className) {
    const candidate = system.parseCandidate(className)[0];
    if(!candidate) return null;
    return candidate.root ?? candidate.kind;
}

/*
 * The base class pools, each a map from root to the classes under it, so a draw takes a root first.
 */
const bareClassNames = new Set(bareSystem.getClassList().map(([className]) => className));
const bareRoots = new Set();
for(const className of bareClassNames) {
    const root = rootOf(bareSystem, className);
    if(root !== null) bareRoots.add(root);
}
const modifiersByClass = new Map();
const ownUtilityPool = new Map();
const ownKeyPool = new Map();
const frameworkPool = new Map();
function addTo(pool, root, className) {
    if(!pool.has(root)) pool.set(root, []);
    pool.get(root).push(className);
}
for(const [className, details] of designSystem.getClassList()) {
    if(!compiles(className)) continue;
    const root = rootOf(designSystem, className);
    if(root === null) continue;
    if(details?.modifiers?.length) modifiersByClass.set(className, details.modifiers);
    if(bareClassNames.has(className)) addTo(frameworkPool, root, className);
    else if(bareRoots.has(root)) addTo(ownKeyPool, root, className);
    else addTo(ownUtilityPool, root, className);
}
function sortedRoots(pool) {
    return Array.from(pool.keys()).sort();
}
const ownUtilityRoots = sortedRoots(ownUtilityPool);
const ownKeyRoots = sortedRoots(ownKeyPool);
const frameworkRoots = sortedRoots(frameworkPool);
if(frameworkRoots.length === 0 || ownUtilityRoots.length + ownKeyRoots.length === 0) {
    process.stderr.write(
        `${recordedEntryPoint}: ${frameworkRoots.length} framework roots and ` +
            `${ownUtilityRoots.length + ownKeyRoots.length} of its own; a corpus needs both\n`,
    );
    process.exit(1);
}
function drawFrom(pool, roots) {
    return pick(pool.get(pick(roots)));
}

/*
 * The variant pools. A variant is kept when `<variant>:flex` compiles; a stack is checked again as a whole
 * on the class it wraps, since not every pair stacks.
 */
function variantCompiles(variant) {
    return compiles(`${variant}:flex`);
}
const bareVariants = new Map(bareSystem.getVariants().map((variant) => [variant.name, variant]));
const frameworkStaticVariants = [];
const frameworkFunctionalVariants = [];
const ownVariants = [];
for(const variant of designSystem.getVariants()) {
    const bareVariant = bareVariants.get(variant.name);
    if(variant.values.length === 0) {
        if(!variantCompiles(variant.name)) continue;
        // Overridden (`dark` on a theme that declares its own) or new: either way the theme's.
        const isOwn =
            !bareVariant ||
            designSystem.candidatesToCss([`${variant.name}:flex`])[0] !==
                bareSystem.candidatesToCss([`${variant.name}:flex`])[0];
        (isOwn ? ownVariants : frameworkStaticVariants).push(variant.name);
        continue;
    }
    const bareValues = new Set(bareVariant?.values ?? []);
    for(const value of variant.values) {
        const spelled = variant.hasDash ? `${variant.name}-${value}` : `${variant.name}${value}`;
        if(!variantCompiles(spelled)) continue;
        (bareValues.has(value) ? frameworkFunctionalVariants : ownVariants).push(spelled);
    }
}
const arbitraryVariants = [
    'data-[state=open]',
    'data-[side=top]',
    'aria-[sort=ascending]',
    'min-[40rem]',
    'max-[30rem]',
    'has-[>img]',
    'supports-[display:grid]',
    'nth-3',
    'nth-last-2',
    'nth-of-type-2',
    '[&>*]',
    '[&_svg]',
    'group-[.is-open]',
    'group-hover/item',
    'peer-checked/toggle',
    'not-[.active]',
    'in-[.shelf]',
].filter(variantCompiles);

function drawVariant() {
    if(ownVariants.length > 0 && random() < 0.3) return pick(ownVariants);
    return pick(
        weighted([
            [frameworkStaticVariants, 4],
            [frameworkFunctionalVariants, 4],
            [arbitraryVariants, 2],
        ]),
    );
}
function drawVariants() {
    const count = weighted([
        [0, 50],
        [1, 30],
        [2, 15],
        [3, 5],
    ]);
    const variants = [];
    while(variants.length < count) {
        const variant = drawVariant();
        if(!variants.includes(variant)) variants.push(variant);
    }
    return variants;
}

/*
 * Arbitrary values, numbers drawn so the same template writes many classes.
 */
function hexColor() {
    return Array.from({ length: 6 }, () => '0123456789abcdef'[between(0, 15)]).join('');
}
const arbitraryTemplates = [
    () => `w-[${between(7, 97)}px]`,
    () => `h-[${between(7, 97)}px]`,
    () => `min-h-[${between(20, 90)}vh]`,
    () => `max-w-[${between(20, 72)}rem]`,
    () => `p-[${between(1, 19)}px]`,
    () => `top-[${between(1, 99)}%]`,
    () => 'grid-cols-[1fr_auto]',
    () => `grid-cols-[${between(8, 24)}rem_1fr]`,
    () => `bg-[#${hexColor()}]`,
    () => `text-[${between(10, 30)}px]`,
    () => `text-[#${hexColor()}]`,
    () => `border-[${between(1, 6)}px]`,
    () => `rounded-[${between(2, 24)}px]`,
    () => `tracking-[0.0${between(1, 9)}em]`,
    () => `z-[${between(2, 60)}]`,
    () => `translate-x-[${between(1, 40)}px]`,
    () => `shadow-[0_1px_${between(2, 30)}px_#0000001a]`,
    () => `duration-[${between(1, 9)}00ms]`,
    () => '[mask-type:luminance]',
];
// The theme's own functional roots that take an arbitrary value, found by asking rather than assumed.
const ownArbitraryTemplates = [];
for(const root of ownUtilityRoots) {
    if(compiles(`${root}-[13px]`)) ownArbitraryTemplates.push(() => `${root}-[${between(3, 40)}px]`);
    else if(compiles(`${root}-[#a1b2c3]`)) ownArbitraryTemplates.push(() => `${root}-[#${hexColor()}]`);
}

function drawBase() {
    const roll = random();
    if(roll < 0.4) {
        // The theme's own, half from its `@utility` roots and half built on its `@theme` keys.
        if(ownArbitraryTemplates.length > 0 && random() < 0.08) return pick(ownArbitraryTemplates)();
        const useUtility = ownKeyRoots.length === 0 || (ownUtilityRoots.length > 0 && random() < 0.5);
        return useUtility ? drawFrom(ownUtilityPool, ownUtilityRoots) : drawFrom(ownKeyPool, ownKeyRoots);
    }
    if(roll < 0.48) return pick(arbitraryTemplates)();
    const className = drawFrom(frameworkPool, frameworkRoots);
    if(random() < 0.08 && !className.startsWith('-') && compiles(`-${className}`)) return `-${className}`;
    return className;
}
function drawClass() {
    for(let attempt = 0; attempt < 50; attempt++) {
        let className = drawBase();
        const modifiers = modifiersByClass.get(className);
        if(modifiers && random() < 0.25) className = `${className}/${pick(modifiers)}`;
        if(random() < 0.05) className = `${className}!`;
        const variants = drawVariants();
        const written = [...variants, className].join(':');
        if(compiles(written)) return written;
    }
    throw new Error(`${recordedEntryPoint}: fifty draws in a row wrote nothing Tailwind compiles`);
}

/*
 * The properties a class sets under its variants, read from the CSS Tailwind writes for it. Custom properties
 * and `@property` descriptors are left out: they are how utilities compose, not what a list sets twice.
 */
const propertyDescriptors = new Set(['syntax', 'inherits', 'initial-value']);
function settings(className) {
    const css = designSystem.candidatesToCss([className])[0] ?? '';
    const variantPrefix = className.slice(0, className.length - baseOf(className).length);
    const keys = new Set();
    for(const match of css.matchAll(/([a-z-]+)\s*:\s*[^;{}]+;/g)) {
        const property = match[1];
        if(property.startsWith('--') || propertyDescriptors.has(property)) continue;
        keys.add(`${variantPrefix}|${property}`);
    }
    return keys;
}
// The class after its variants: the last colon outside brackets.
function baseOf(className) {
    let depth = 0;
    let lastColon = -1;
    for(let index = 0; index < className.length; index++) {
        const character = className[index];
        if(character === '[' || character === '(') depth++;
        else if(character === ']' || character === ')') depth--;
        else if(character === ':' && depth === 0) lastColon = index;
    }
    return className.slice(lastColon + 1);
}

/*
 * The dead classes: on each of a few of the theme's own functional roots, the first value no key answers.
 */
const deadClasses = [];
{
    const candidates = [];
    for(const root of ownUtilityRoots) {
        // A root declared both ways lists its static first, so any functional class under it will do.
        const isFunctional = ownUtilityPool
            .get(root)
            .some((className) => designSystem.parseCandidate(className)[0]?.kind === 'functional');
        if(!isFunctional) continue;
        for(const value of ['4', '7', '9', 'huge']) {
            const className = `${root}-${value}`;
            if(parsesButCompilesNothing(className)) {
                candidates.push(className);
                break;
            }
        }
    }
    const chosen = new Set();
    while(chosen.size < Math.min(deadRootCount, candidates.length)) chosen.add(pick(candidates));
    for(const className of Array.from(chosen).sort()) {
        let wrapped;
        do {
            wrapped = `${pick(frameworkStaticVariants)}:${className}`;
        } while(!parsesButCompilesNothing(wrapped));
        deadClasses.push(className, wrapped);
    }
}
/*
 * The planted collapses: a few pairs Tailwind's canonicalizer merges into one class (`mt-gutter mb-gutter`
 * to `my-gutter`), so enforce-canonical-classes has findings to be measured on, and a rule that stopped
 * merging fails as one that merges too much does. Asked with better-tailwindcss's defaults, the options
 * tools/generate_canonical_singles asks with. Every other list is one the canonicalizer merges nothing in.
 */
const canonicalOptions = { collapse: true, logicalToPhysical: true };
function canonicalLength(classes) {
    // Unknown classes are dropped before the canonicalizer, as better-tailwindcss drops them, since
    // Tailwind crashes on some of them.
    const live = classes.filter((className) => !deadClasses.includes(className));
    try {
        return designSystem.canonicalizeCandidates(live, canonicalOptions).length + classes.length - live.length;
    }
    catch {
        return -1;
    }
}
const collapses = [];
{
    const values = ['4', '2.5', 'px'];
    for(const className of ownKeyPool.get('p') ?? []) values.push(className.slice('p-'.length));
    const candidates = [];
    for(const [first, second] of [
        ['mt', 'mb'],
        ['ml', 'mr'],
        ['pt', 'pb'],
        ['px', 'py'],
        ['w', 'h'],
    ]) {
        for(const value of values) {
            const inputs = [`${first}-${value}`, `${second}-${value}`];
            if(!inputs.every(compiles) || canonicalLength(inputs) !== 1) continue;
            candidates.push({ inputs, output: designSystem.canonicalizeCandidates(inputs, canonicalOptions)[0] });
        }
    }
    const chosen = new Set();
    while(chosen.size < Math.min(collapseCount, candidates.length)) chosen.add(pick(candidates));
    collapses.push(...Array.from(chosen).sort((left, right) => (left.output < right.output ? -1 : 1)));
}

// Each dead class and each collapse goes into one list of its own, spread through the tree rather than
// bunched at the front.
const plantedByList = new Map();
deadClasses.forEach((className, index) => {
    plantedByList.set(Math.floor(((index + 0.5) * listCount) / deadClasses.length), { dead: className });
});
collapses.forEach((collapse, index) => {
    let listIndex = Math.floor(((index + 0.25) * listCount) / collapses.length);
    while(plantedByList.has(listIndex)) listIndex++;
    plantedByList.set(listIndex, { collapse });
});

/*
 * The lists.
 */
const lists = [];
const seenLists = new Set();
while(lists.length < listCount) {
    const length = weighted([
        [2, 10],
        [3, 16],
        [4, 18],
        [5, 18],
        [6, 14],
        [7, 10],
        [8, 8],
        [9, 6],
    ]);
    const planted = plantedByList.get(lists.length);
    const plantedClasses = planted?.dead ? [planted.dead] : (planted?.collapse.inputs ?? []);
    const classes = [...plantedClasses];
    const taken = new Set();
    for(const className of plantedClasses) {
        for(const key of settings(className)) taken.add(key);
    }
    let attempts = 0;
    while(classes.length < Math.max(length, plantedClasses.length + 1) && attempts++ < 200) {
        const className = drawClass();
        if(classes.includes(className)) continue;
        const keys = settings(className);
        if(Array.from(keys).some((key) => taken.has(key))) continue;
        for(const key of keys) taken.add(key);
        classes.push(className);
    }
    // A planted class goes first in its list only by construction; spread each to a drawn position.
    for(const className of plantedClasses) {
        classes.splice(classes.indexOf(className), 1);
        classes.splice(Math.floor(random() * (classes.length + 1)), 0, className);
    }
    const key = classes.join(' ');
    if(classes.length < 2 || seenLists.has(key)) continue;
    // The canonicalizer merges the planted pair and nothing else, so the collapses are exactly the planted.
    if(canonicalLength(classes) !== classes.length - (planted?.collapse ? 1 : 0)) continue;
    seenLists.add(key);
    lists.push(classes);
}

/*
 * The files: invented component names, forty lists each, in the order drawn.
 */
const adjectives = [
    'Amber',
    'Brisk',
    'Cobalt',
    'Dusky',
    'Ember',
    'Fable',
    'Gilded',
    'Hollow',
    'Ivory',
    'Jade',
    'Kindled',
    'Lunar',
    'Misty',
    'Nimble',
    'Opal',
    'Pebble',
    'Quiet',
    'Russet',
    'Sable',
    'Tidal',
];
const nouns = ['Lantern', 'Ledger', 'Meadow', 'Orchard', 'Quiver', 'Spindle', 'Thicket', 'Wicket'];
const files = new Map();
for(let index = 0; index * listsPerFile < lists.length; index++) {
    const adjective = adjectives[index % adjectives.length];
    const name = `${adjective}${nouns[Math.floor(index / adjectives.length) % nouns.length]}`;
    const body = lists
        .slice(index * listsPerFile, (index + 1) * listsPerFile)
        .map((classes) => `            <div className="${classes.join(' ')}" />`)
        .join('\n');
    files.set(
        `${name}.tsx`,
        `// Generated from ${recordedEntryPoint} by tools/generate_planted_repository; regenerate rather than edit.\n` +
            `export function ${name}() {\n` +
            `    return (\n` +
            `        <section>\n` +
            `${body}\n` +
            `        </section>\n` +
            `    );\n` +
            `}\n`,
    );
}
const classCount = lists.reduce((sum, classes) => sum + classes.length, 0);
files.set(
    manifestName,
    JSON.stringify(
        {
            tailwindVersion,
            entryPoint: recordedEntryPoint,
            lists: lists.length,
            classes: classCount,
            deadClasses,
            collapses,
        },
        null,
        4,
    ) + '\n',
);

function writeTree(directory) {
    NodeFileSystem.mkdirSync(directory, { recursive: true });
    for(const [name, contents] of Array.from(files).sort(([left], [right]) => (left < right ? -1 : 1))) {
        NodeFileSystem.writeFileSync(NodePath.join(directory, name), contents);
    }
}

const output = locate(outputArgument);
const summary =
    `${lists.length} lists, ${classCount} classes, ${deadClasses.length} dead (${deadClasses.join(' ')}), ` +
    `${collapses.length} collapses (${collapses.map((collapse) => collapse.output).join(' ')}), ` +
    `${files.size - 1} files`;

if(check) {
    const temporary = NodeFileSystem.mkdtempSync(NodePath.join(NodeOperatingSystem.tmpdir(), 'planted-'));
    writeTree(temporary);
    const fresh = NodeFileSystem.readdirSync(temporary).sort();
    const committed = NodeFileSystem.existsSync(output.path) ? NodeFileSystem.readdirSync(output.path).sort() : [];
    const differences = [];
    for(const name of new Set([...fresh, ...committed])) {
        if(!committed.includes(name)) differences.push(`${name} is missing from the committed tree`);
        else if(!fresh.includes(name)) {
            differences.push(`${name} is in the committed tree and this generator writes no such file`);
        }
        else if(
            !NodeFileSystem.readFileSync(NodePath.join(temporary, name)).equals(
                NodeFileSystem.readFileSync(NodePath.join(output.path, name)),
            )
        ) {
            differences.push(`${name} differs`);
        }
    }
    for(const name of fresh) NodeFileSystem.unlinkSync(NodePath.join(temporary, name));
    NodeFileSystem.rmdirSync(temporary);
    if(differences.length > 0) {
        process.stderr.write(
            `${output.recorded} is stale against ${recordedEntryPoint} on Tailwind ${tailwindVersion}:\n` +
                differences.map((line) => `  ${line}\n`).join('') +
                'Re-run without --check and read the diff.\n',
        );
        process.exit(1);
    }
    process.stderr.write(`${output.recorded} matches ${recordedEntryPoint}: ${summary}\n`);
}
else {
    // A file this run would not write is a leftover from an older shape; it is named, not deleted.
    if(NodeFileSystem.existsSync(output.path)) {
        const leftovers = NodeFileSystem.readdirSync(output.path).filter((name) => !files.has(name));
        if(leftovers.length > 0) {
            process.stderr.write(
                `${output.recorded} holds files this generator does not write, remove them first: ` +
                    `${leftovers.join(', ')}\n`,
            );
            process.exit(1);
        }
    }
    writeTree(output.path);
    process.stderr.write(`wrote ${output.recorded} from ${recordedEntryPoint}: ${summary}\n`);
}
