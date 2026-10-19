/*
 * Recover every framework utility's suggestion recipe from the engine, so the Go side can build
 * Tailwind's class list for any design system without running JavaScript (#15swfzq unit 1b).
 *
 * `getClassList()` is the population upstream's arbitraryUtilities pass searches: every class it lists
 * whose signature equals the written class's is a candidate replacement, and two non-negative ones
 * make the pass bail. So a Go port of that pass needs the same population, built from the run's own
 * theme. The theme half is `keysInNamespaces` over the root's namespaces, which the Go Theme already
 * answers. The framework half is what this extracts: per root, the suggestion groups its `suggest`
 * call registers, each with its literal values, its theme namespaces, whether fractions and a bare
 * default are offered, and the same for its modifiers.
 *
 * # Probed rather than read from the source
 *
 * A group's theme keys are resolved by the time `getCompletions` returns them, so a value from the
 * theme and a literal value look alike. This loads a design system whose theme holds exactly one
 * probe key per namespace, `--<namespace>-zzp<index>`, and nothing else. A returned value that is a
 * probe names its namespace; anything else is a literal. A key from a nested namespace shows up under
 * its parent too (`--text-color-zzp7` is `color-zzp7` to `--text`), which is upstream's behaviour,
 * and the namespace is recovered by taking the value off the end of the probe key.
 *
 * The namespaces probed are every `--name` string the installed bundle holds, minus `--tw-*`. A
 * namespace this misses would leave a root's theme keys out of the Go class list, and the class-list
 * differential over three real themes is what catches it.
 *
 * # Conditional groups
 *
 * Five registrations read the theme inside `suggest`: two offer the default spacing scale only when
 * `--spacing` is set, and three offer a bare default only when their own key is. Each is found by
 * loading again with that key absent and recording which group changed, rather than by naming the
 * five here.
 *
 * Usage:
 *
 *   node internal/lint/rules/tailwind/tools/generate_suggestions/enumerate.mjs --resolve-root <dir>
 *
 * The resolve root supplies the tailwindcss install, which is the only thing read from it.
 */

import NodeFileSystem from 'node:fs';
import NodeModule from 'node:module';
import NodePath from 'node:path';
import { loadDesignSystem } from '../generate_descriptors/loader.mjs';
import { locate } from '../corpus.mjs';

function flagValue(name) {
    const index = process.argv.indexOf(name);
    return index === -1 ? undefined : process.argv[index + 1];
}

const resolveRootArgument = flagValue('--resolve-root');
if (!resolveRootArgument) {
    process.stderr.write('usage: enumerate.mjs --resolve-root <dir>\n');
    process.exit(2);
}
const resolveRoot = locate(resolveRootArgument).path;

/*
 * The namespaces, read off the installed bundle. Every theme key a registration names is a string
 * literal in the bundle, as it is in utilities.ts, so this is the source's list without the source.
 */
const requireFromResolveRoot = NodeModule.createRequire(NodePath.join(resolveRoot, 'noop.js'));
const bundle = NodeFileSystem.readFileSync(requireFromResolveRoot.resolve('tailwindcss'), 'utf8');
const namespaces = [
    ...new Set(
        [...bundle.matchAll(/["'`](--[a-z][a-z0-9-]*)["'`]/g)]
            .map((match) => match[1])
            .filter((name) => !name.startsWith('--tw-')),
    ),
].sort();

/*
 * The keys whose presence decides a conditional group. Probed one at a time, so each condition is
 * attributed to the key that causes it rather than to the set.
 */
const conditionKeys = ['--spacing', '--shadow', '--inset-shadow', '--text-shadow'];
const conditionValues = { '--spacing': '0.25rem', '--shadow': '0 0 1px red', '--inset-shadow': '0 0 1px red', '--text-shadow': '0 0 1px red' };

/*
 * A condition key is a theme key like any other, so it leaks into the namespaces it sits under, as
 * upstream's keysInNamespaces does. decode is told which condition keys are present to tell a leaked
 * key from a literal.
 */
async function loadProbeSystem(presentConditionKeys) {
    const lines = namespaces.map((namespace, index) => `  ${namespace}-zzp${index}: 1px;`);
    for (const key of presentConditionKeys) {
        lines.push(`  ${key}: ${conditionValues[key]};`);
    }
    const source = `@theme {\n${lines.join('\n')}\n}\n`;
    // Any real path anchors `base`; the probe system reads no stylesheet beside its own source.
    const anchor = NodePath.join(resolveRoot, 'package.json');
    const { designSystem, tailwindVersion } = await loadDesignSystem(anchor, resolveRootArgument, source);
    return { designSystem, tailwindVersion, presentConditionKeys };
}

const everything = await loadProbeSystem(conditionKeys);
const { tailwindVersion } = everything;
const withoutEach = {};
for (const key of conditionKeys) {
    withoutEach[key] = await loadProbeSystem(conditionKeys.filter((other) => other !== key));
}

const suggestedFractions = [
    '1/2', '1/3', '2/3', '1/4', '2/4', '3/4', '1/5', '2/5', '3/5', '4/5', '1/6', '2/6', '3/6', '4/6', '5/6',
    '1/12', '2/12', '3/12', '4/12', '5/12', '6/12', '7/12', '8/12', '9/12', '10/12', '11/12',
];

const problems = [];

/*
 * One list of suggestions decoded into its recipe. Upstream builds a list as literal values, then
 * theme keys, then the fractions when offered, with a bare `null` put in front for a default, so the
 * fractions are recognised as that exact tail.
 */
function decode(system, root, list) {
    const recipe = { literals: [], namespaces: [], fractions: false, hasDefault: false };
    let values = list;
    const tail = values.slice(-suggestedFractions.length);
    if (values.length >= suggestedFractions.length && tail.every((value, index) => value === suggestedFractions[index])) {
        recipe.fractions = true;
        values = values.slice(0, -suggestedFractions.length);
    }
    // Probes first, so the namespaces this list draws on are known before a non-probe value is judged.
    const rest = [];
    for (const value of values) {
        if (value === null) {
            recipe.hasDefault = true;
            continue;
        }
        const probe = /zzp(\d+)$/.exec(value);
        if (!probe) {
            rest.push(value);
            continue;
        }
        const key = `${namespaces[Number(probe[1])]}-zzp${probe[1]}`;
        if (!key.endsWith(`-${value}`)) {
            problems.push(`${root}: suggestion ${value} names a probe it cannot have come from`);
            continue;
        }
        const namespace = key.slice(0, key.length - value.length - 1);
        if (!recipe.namespaces.includes(namespace)) recipe.namespaces.push(namespace);
    }
    /*
     * A value spelling a present condition key under one of this list's own namespaces is that key,
     * leaked: `inset` suggests `shadow` while `--inset-shadow` is set. Only this list's namespaces count,
     * because a literal can spell the same word: `transition` offers `shadow` outright, and
     * `--inset-shadow` says nothing about a root that draws on no `--inset` keys.
     */
    for (const value of rest) {
        const leaked = recipe.namespaces.some((namespace) => system.presentConditionKeys.includes(`${namespace}-${value}`));
        if (!leaked) recipe.literals.push(value);
    }
    return recipe;
}

let defaultSpacingSuggestions = null;
const functional = {};
for (const root of everything.designSystem.utilities.keys('functional')) {
    const groups = everything.designSystem.utilities.getCompletions(root);
    if (groups.length === 0) continue;
    functional[root] = groups.map((group, groupIndex) => {
        // Compared against as found; the entry's own lists are the ones adjusted below.
        const values = decode(everything, root, group.values);
        const modifiers = decode(everything, root, group.modifiers);
        const entry = {
            supportsNegative: group.supportsNegative === true,
            values: decode(everything, root, group.values),
            modifiers: decode(everything, root, group.modifiers),
        };

        for (const key of conditionKeys) {
            const other = withoutEach[key].designSystem.utilities.getCompletions(root)[groupIndex];
            if (!other) {
                problems.push(`${root}: group ${groupIndex} disappears without ${key}`);
                continue;
            }
            const otherValues = decode(withoutEach[key], root, other.values);
            if (otherValues.hasDefault !== values.hasDefault) {
                if (entry.values.defaultWhenThemeKey) problems.push(`${root}: default depends on two keys`);
                entry.values.defaultWhenThemeKey = key;
            }
            if (otherValues.literals.length !== values.literals.length) {
                if (key !== '--spacing') {
                    problems.push(`${root}: literals depend on ${key}, which only --spacing is known to decide`);
                    continue;
                }
                // The spacing scale is appended to the literals the group always has.
                const added = values.literals.slice(otherValues.literals.length);
                if (otherValues.literals.some((value, index) => value !== values.literals[index])) {
                    problems.push(`${root}: the spacing scale is not a tail of the literals`);
                }
                if (defaultSpacingSuggestions === null) defaultSpacingSuggestions = added;
                else if (JSON.stringify(defaultSpacingSuggestions) !== JSON.stringify(added)) {
                    problems.push(`${root}: a second spacing scale differs from the first`);
                }
                entry.values.literals = otherValues.literals;
                entry.values.spacingScale = true;
            }
            const otherModifiers = decode(withoutEach[key], root, other.modifiers);
            if (JSON.stringify(otherModifiers) !== JSON.stringify(modifiers)) {
                problems.push(`${root}: modifiers depend on ${key}`);
            }
        }
        return entry;
    });
}

/*
 * Every static the registry holds, and whether the class list holds it: upstream keeps a static whose
 * completions are non-empty, and a static with no `suggest` of its own answers one empty group. Read
 * from the registry rather than from FrameworkStaticDeclarations, which leaves out the statics whose
 * body reads the theme, `transition-shadow` among them, since it holds only bodies every system agrees on.
 */
const staticNames = everything.designSystem.utilities.keys('static').filter((name) => typeof name === 'string').sort();
const statics = Object.fromEntries(
    staticNames.map((name) => [name, everything.designSystem.utilities.getCompletions(name).length > 0]),
);

if (problems.length > 0) {
    process.stderr.write(`enumerate.mjs: ${problems.length} problems\n${problems.join('\n')}\n`);
    process.exit(1);
}

process.stdout.write(
    JSON.stringify(
        {
            tailwindVersion,
            groundTruth: 'utilities.getCompletions(root) under a probe theme of one key per namespace',
            probedNamespaces: namespaces.length,
            defaultSpacingSuggestions,
            suggestedFractions,
            statics,
            functional,
        },
        null,
        2,
    ) + '\n',
);
