/*
 * What better-tailwindcss reads as a class string, asked of better-tailwindcss itself (#btxd64n).
 *
 * Two answers, both from the installed package rather than transcribed:
 *
 *   - its default selectors, options/default-options.js's DEFAULT_SELECTORS, which cohere embeds as
 *     its own defaults, so the defaults are upstream's by construction and a release that changes them
 *     shows up as a diff;
 *   - for each case in the corpus, the literals its own createRuleListener hands a rule, through
 *     createRule, so the options and settings merge is upstream's too. A probe rule captures them
 *     under ESLint's Linter with typescript-eslint's parser.
 *
 * The literal ranges are what the Go reader is compared against, range for range. Upstream reads a
 * template with holes as one literal per quasi, so a quasi's range runs from its opening delimiter to
 * its closing one, `p-2 ${` and `} m-1`, which is the token range typescript-go gives a template's
 * head, middle and tail.
 *
 * Usage: node extract.mjs <packages> <cases.json>
 *
 * <packages> is a directory, or a corpus spelling such as ahra:., where eslint, the plugin and
 * typescript-eslint's parser resolve. Writes the JSON to standard output.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodeModule from 'node:module';
import * as NodePath from 'node:path';
import * as NodeUrl from 'node:url';
import { locate } from '../corpus.mjs';

const [packagesArgument, casesArgument] = process.argv.slice(2);
if (!packagesArgument || !casesArgument) {
    throw new Error('usage: node extract.mjs <packages> <cases.json>');
}

const packagesRoot = locate(packagesArgument).path;
const requireFromPackages = NodeModule.createRequire(NodePath.join(packagesRoot, 'noop.js'));

/*
 * The plugin's exports map names neither its package.json nor its lib modules, so its directory is
 * found from its entry, lib/configs/config.js, and its modules are imported by path.
 */
const pluginLibrary = NodePath.resolve(NodePath.dirname(requireFromPackages.resolve('eslint-plugin-better-tailwindcss')), '..');
const pluginVersion = JSON.parse(NodeFileSystem.readFileSync(NodePath.join(pluginLibrary, '..', 'package.json'), 'utf8')).version;
const importFromLibrary = (path) => import(NodeUrl.pathToFileURL(NodePath.join(pluginLibrary, path)).href);

const { createRule } = await importFromLibrary('utils/rule.js');
const { DEFAULT_SELECTORS } = await importFromLibrary('options/default-options.js');
const { Linter } = await import(NodeUrl.pathToFileURL(requireFromPackages.resolve('eslint')).href);
const parser = await import(NodeUrl.pathToFileURL(requireFromPackages.resolve('@typescript-eslint/parser')).href);

let captured = [];
const probe = createRule({
    category: 'stylistic',
    description: 'Captures the literals better-tailwindcss reads.',
    docs: '',
    lintLiterals: (_, literals) => {
        captured.push(...literals);
    },
    name: 'probe',
    recommended: false,
});

const cases = JSON.parse(NodeFileSystem.readFileSync(NodePath.resolve(casesArgument), 'utf8'));
const results = [];
for (const testCase of cases) {
    const code = testCase.code.join('\n');
    // ESLint's ranges count UTF-16 units and typescript-go's count bytes. They agree on ASCII alone.
    if (!/^[\x00-\x7f]*$/.test(code)) {
        throw new Error(`case "${testCase.name}" is not ASCII, so its ranges would not compare`);
    }
    captured = [];
    const linter = new Linter({ configType: 'flat', cwd: packagesRoot });
    const messages = linter.verify(
        code,
        [
            {
                files: ['**/*.tsx'],
                languageOptions: { parser, parserOptions: { ecmaFeatures: { jsx: true } } },
                plugins: { probe: { rules: { probe: probe.rule } } },
                rules: { 'probe/probe': ['error', testCase.options ?? {}] },
                settings: testCase.settings ? { 'better-tailwindcss': testCase.settings } : {},
            },
        ],
        'Case.tsx',
    );
    if (messages.length > 0) {
        throw new Error(`case "${testCase.name}": ${messages.map((message) => message.message).join('; ')}`);
    }

    // Upstream deduplicates within one listener's call, never across two, so a string two listeners
    // reach is linted twice. The comparison is of which strings are read, so it is a set.
    const seen = new Set();
    const literals = [];
    for (const literal of captured) {
        const key = `${literal.range[0]}:${literal.range[1]}`;
        if (seen.has(key)) {
            continue;
        }
        seen.add(key);
        literals.push({
            start: literal.range[0],
            end: literal.range[1],
            text: code.slice(literal.range[0], literal.range[1]),
            concatenatedLeft: literal.isConcatenatedLeft === true,
            concatenatedRight: literal.isConcatenatedRight === true,
        });
    }
    literals.sort((left, right) => left.start - right.start || left.end - right.end);
    results.push({ name: testCase.name, literals });
}

process.stdout.write(
    JSON.stringify({ plugin: pluginVersion, defaultSelectors: DEFAULT_SELECTORS, cases: results }, null, 4) + '\n',
);
