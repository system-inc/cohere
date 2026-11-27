/*
 * Write the goldens strict class order is held to, testdata/strict_variants.json: for each class, the variants
 * upstream groups it by.
 *
 * eslint-plugin-better-tailwindcss 4.7.0's strict order groups a class list by each class's variants, read by its
 * getDissectedClasses and, when Tailwind cannot parse the class, by the rule's colon fallback. Both come down to
 * this, which is what class_order_strict_test.go documents and what this file runs:
 *
 *   const [parsed] = designSystem.parseCandidate(className);
 *   const variants = parsed?.variants?.map((variant) => designSystem.printVariant(variant)).reverse()
 *       ?? className.match(/^(.*):/)?.[1]?.split(':') ?? [];
 *
 * # The classes
 *
 * Every class in the two source trees generated in cohere, collapse/testdata/public_repository/source and
 * collapse/testdata/planted_repository/source, that carries a variant, harvested the way the corpus generators
 * harvest them, plus the edge cases below, which no generated tree writes: arbitrary selectors, `&:is(...)`, named
 * groups, container queries, the `(--x)` shorthand, and strings that do not parse at all and reach the fallback.
 *
 * # The theme
 *
 * Read on the planted theme, which declares variants of its own (`compact`, `pressed`, `tablet`), and checked on
 * the public theme. The test asks both systems for the same goldens, which holds only for a class both themes read
 * alike: a class that parses on one theme and reaches the fallback on the other, with a printed form that differs
 * from its written one, has no single answer. Such a class is left out and counted on stderr, so the file holds
 * only goldens both systems are bound by.
 *
 * Usage, from the repository root:
 *
 *   node internal/lint/rules/tailwind/tools/generate_strict_variants/generate.mjs \
 *       internal/lint/rules/tailwind/collapse/testdata/planted_repository/styles/theme.css \
 *       internal/lint/rules/tailwind/collapse/testdata/public_theme/theme.css \
 *       internal/lint/rules/tailwind/collapse/testdata/public_repository/source \
 *       internal/lint/rules/tailwind/collapse/testdata/planted_repository/source \
 *       [--resolve-root <dir holding node_modules/tailwindcss>] \
 *       > internal/lint/rules/tailwind/testdata/strict_variants.json
 *
 * Writes the file to stdout and its counts to stderr.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodePath from 'node:path';
import { locate } from '../corpus.mjs';
import { loadDesignSystem } from '../generate_descriptors/loader.mjs';

const usage = 'usage: generate.mjs <theme.css> <cross-check theme.css> <source dir>... [--resolve-root <dir>]\n';
const positionalArguments = [];
let resolveRootArgument;
for(let index = 2; index < process.argv.length; index++) {
    const argument = process.argv[index];
    if(argument === '--resolve-root') {
        resolveRootArgument = process.argv[++index];
        continue;
    }
    positionalArguments.push(argument);
}
if(positionalArguments.length < 3 || resolveRootArgument === '') {
    process.stderr.write(usage);
    process.exit(2);
}
const [themeArgument, crossCheckThemeArgument, ...treeArguments] = positionalArguments;

/*
 * Shapes no generated tree writes, kept from the goldens' first capture because each reaches a different branch of
 * the reading. None names a design system's own token, so each reads the same on any theme.
 */
const edgeCases = [
    // Arbitrary selectors, which print as written, and `&:is(...)`, which prints as its inner selector.
    '[.a_&]:flex',
    '[@media(min-width:100px)]:flex',
    '[&_.a\\_b]:flex',
    '[&_p]:my-0',
    '[&_svg]:hidden',
    '[&::-webkit-inner-spin-button]:appearance-none',
    '[&:is(p)]:flex',
    '[&[data-state=open]>svg]:rotate-180',
    '[&>*]:flex',
    '[&>div]:block!',
    '[&>td]:...',
    // Arbitrary properties, which carry no variant, one under a variant.
    '[appearance:textfield]',
    '[font:inherit]',
    'md:[--col-foo:12rem]',
    // Container queries, named and arbitrary.
    '@[30rem]:flex',
    '@lg:flex',
    '@min-md:flex',
    // The universal child variant, alone and inside an arbitrary one.
    '*:flex',
    'data-[attr=a]:*:text-black',
    // Named groups, static and functional.
    'group-hover/item:flex',
    'group-data-[x=y]/n:flex',
    // The `(--x)` shorthand, an escaped underscore and a quoted feature query.
    'data-(--x):flex',
    'data-[x=a\\_b]:flex',
    'supports-[display:grid]:flex',
    'supports-[font-variation-settings:"wght"_400]:flex',
    // Functional and compound framework variants.
    'aria-checked:flex',
    'min-[100px]:flex',
    'max-sm:flex',
    'nth-3:flex',
    'not-hover:flex',
    'in-focus:flex',
    'in-[pre]:p-0',
    'has-[>img]:flex',
    'peer-[.is-dirty]:flex',
    'group-focus-within:disabled:opacity-50',
    // Stacked, both ways round.
    'hover:dark:text-black',
    'md:hover:text-black',
    'dark:group-hover/item:text-blue-400',
    // Strings that do not parse, which reach the colon fallback: a variant no theme registers, a property written
    // as a class, a selector or URL caught in a class list, a class cut off mid-bracket.
    'not-a-variant:flex',
    'custom-variant:custom-class',
    'blur:4',
    'display:block',
    'height:100%',
    'min-width:auto,',
    'opacity:0,',
    'overflow:hidden>',
    'input:not([type=',
    'select:not([disabled]),',
    '[tabindex]:not([tabindex=',
    'b([-a-zA-Z0-9()@:%_+.',
    'data:image/svg+xml,%3Csvg',
    'file:/,',
    'folder:foo/',
    'formats:&#10',
    'group-hover:)',
    'before:content-[',
    'h:mm',
    'sort=status:asc,amount:desc',
];

/* The reading upstream's strict order groups by, exactly as documented above. */
function strictVariantsOf(designSystem, className) {
    let parsed;
    try {
        [parsed] = designSystem.parseCandidate(className);
    }
    catch {
        parsed = undefined;
    }
    return (
        parsed?.variants?.map((variant) => designSystem.printVariant(variant)).reverse() ??
        className.match(/^(.*):/)?.[1]?.split(':') ??
        []
    );
}

/* Every distinct `className="..."` token under a tree, as tools/generate_class_order harvests a corpus. */
function harvest(treeArgument) {
    const classNames = new Set();
    function scanDirectory(directory) {
        for(const entry of NodeFileSystem.readdirSync(directory, { withFileTypes: true })) {
            const fullPath = NodePath.join(directory, entry.name);
            if(entry.isDirectory()) {
                scanDirectory(fullPath);
                continue;
            }
            if(!entry.name.endsWith('.tsx')) continue;
            for(const match of NodeFileSystem.readFileSync(fullPath, 'utf8').matchAll(/className="([^"]*)"/g)) {
                for(const className of match[1].split(/\s+/)) {
                    if(className.length > 0) classNames.add(className);
                }
            }
        }
    }
    scanDirectory(locate(treeArgument).path);
    if(classNames.size === 0) {
        // Loud rather than empty: a tree that yields nothing is a wrong path, and the goldens would shrink to the
        // edge cases without saying so.
        process.stderr.write(`generate.mjs: ${treeArgument} holds no className lists\n`);
        process.exit(3);
    }
    return classNames;
}

const { designSystem, tailwindVersion, recordedEntryPoint } = await loadDesignSystem(
    themeArgument,
    resolveRootArgument,
);
const crossCheck = await loadDesignSystem(crossCheckThemeArgument, resolveRootArgument);

const candidates = new Set(edgeCases);
let treeClasses = 0;
for(const treeArgument of treeArguments) {
    for(const className of harvest(treeArgument)) {
        // A tree class is a golden only when it carries a variant; one without has nothing to group by.
        if(strictVariantsOf(designSystem, className).length === 0) continue;
        if(!candidates.has(className)) treeClasses++;
        candidates.add(className);
    }
}

const classes = [];
const unshared = [];
for(const className of Array.from(candidates).sort()) {
    const variants = strictVariantsOf(designSystem, className);
    const crossCheckVariants = strictVariantsOf(crossCheck.designSystem, className);
    if(variants.join('\u0000') !== crossCheckVariants.join('\u0000')) {
        unshared.push(className);
        continue;
    }
    classes.push({ className, variants });
}

const missingEdgeCases = edgeCases.filter((className) => unshared.includes(className));
if(missingEdgeCases.length > 0) {
    // The edge cases are chosen to read alike on any theme, so one that does not is a reading that changed.
    process.stderr.write(
        `generate.mjs: edge cases read differently on the two themes: ${missingEdgeCases.join(' ')}\n`,
    );
    process.exit(4);
}

process.stderr.write(
    `generate.mjs: ${classes.length} goldens on tailwind ${tailwindVersion}, ${treeClasses} from the trees and ` +
        `${edgeCases.length} edge cases; ${unshared.length} tree classes left out for reading differently on ` +
        `${crossCheck.recordedEntryPoint}${unshared.length > 0 ? `: ${unshared.slice(0, 10).join(' ')}` : ''}\n`,
);

process.stdout.write(
    JSON.stringify(
        {
            source:
                `parseCandidate and printVariant, reversed, with the colon fallback, as eslint-plugin-better-tailwindcss ` +
                `4.7.0's strict order reads them, on Tailwind ${tailwindVersion} with ${recordedEntryPoint} and checked ` +
                `on ${crossCheck.recordedEntryPoint}; generated by tools/generate_strict_variants`,
            classes,
        },
        null,
        1,
    ) + '\n',
);
