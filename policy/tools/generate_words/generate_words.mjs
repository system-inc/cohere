/*
 * Write policy/Words.txt, the English words cohere-go/consistency-require-separated-folder-words splits a folder
 * name into, and policy/Words.NOTICE, the licenses they come under.
 *
 * The words come from two of cspell's dictionaries, both MIT-licensed by Street Side Software: en_US, which holds
 * inflections (`allowed`, `helpers`), and software-terms, which holds what an English list lacks (`workspace`,
 * `javascript`, `toolchain`). Run it from the module root, naming the two package directories, which
 * system/prettier's node_modules already holds:
 *
 *     node policy/tools/generate_words/generate_words.mjs \
 *         ../prettier/node_modules/@cspell/dict-en_us ../prettier/node_modules/@cspell/dict-software-terms
 *
 * en_US ships as a trie only cspell's own library reads, so cspell-trie-lib is found in a node_modules folder at
 * or above the en_US package, as Node would find it for a package installed beside it. Each package's version is
 * checked against the one below, so a newer dictionary is a reviewed change to this file and to Words.txt together,
 * never a silent one.
 *
 * A word is kept when it is ASCII letters alone, lowercased, so `'tis`, `café` and `x86` drop and `Paris` reads as
 * `paris`. A software-terms line opening with `!` is a word cspell forbids, and is skipped; the `*` around a
 * compound part (`*Auth*`) and a leading `~` or `+` are cspell's markers, not part of the word. The house's own
 * supplement, policy/HouseWords.txt, is merged in last.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodePath from 'node:path';
import * as NodeUrl from 'node:url';
import * as NodeZlib from 'node:zlib';

/* The versions Words.txt was generated from. */
const expectedVersions = {
    '@cspell/dict-en_us': '4.4.35',
    '@cspell/dict-software-terms': '5.2.2',
    'cspell-trie-lib': '10.0.1',
};

const [, , englishArgument, softwareArgument] = process.argv;
if (!englishArgument || !softwareArgument) {
    process.stderr.write(
        'usage: generate_words.mjs <@cspell/dict-en_us package directory> <@cspell/dict-software-terms package ' +
            'directory>\n',
    );
    process.exit(2);
}

const policyDirectory = NodePath.resolve(NodePath.dirname(NodeUrl.fileURLToPath(import.meta.url)), '..', '..');
const englishDirectory = NodePath.resolve(englishArgument);
const softwareDirectory = NodePath.resolve(softwareArgument);

/* A package's package.json, refused unless it is the package and version expected. */
function readPackage(directory, name) {
    const packageJson = JSON.parse(NodeFileSystem.readFileSync(NodePath.join(directory, 'package.json'), 'utf8'));
    if (packageJson.name !== name) {
        throw new Error(`${directory} holds ${packageJson.name}, not ${name}`);
    }
    if (packageJson.version !== expectedVersions[name]) {
        throw new Error(
            `${name} is ${packageJson.version}, and Words.txt is generated from ${expectedVersions[name]}; ` +
                'review the new version and update expectedVersions',
        );
    }
    if (packageJson.license !== 'MIT') {
        throw new Error(`${name} is licensed ${packageJson.license}, and this generator was reviewed for MIT alone`);
    }
    return packageJson;
}

/* The nearest cspell-trie-lib at or above directory, as Node's resolution would find it. */
function findTrieLibrary(directory) {
    for (let current = directory; ; current = NodePath.dirname(current)) {
        const candidate = NodePath.join(current, 'node_modules', 'cspell-trie-lib');
        if (NodeFileSystem.existsSync(NodePath.join(candidate, 'package.json'))) {
            return candidate;
        }
        if (NodePath.dirname(current) === current) {
            throw new Error(`no node_modules at or above ${directory} holds cspell-trie-lib`);
        }
    }
}

/* The package's LICENSE, and its copyright line, which Words.txt's header names. */
function readLicense(directory, name) {
    const text = NodeFileSystem.readFileSync(NodePath.join(directory, 'LICENSE'), 'utf8');
    const copyright = text.split('\n').find((line) => line.startsWith('Copyright'));
    if (!copyright) {
        throw new Error(`${name}'s LICENSE names no copyright holder`);
    }
    return { text: text.trimEnd(), copyright: copyright.trim() };
}

/* The word an entry spells, lowercased, or null when it is not ASCII letters alone. */
function wordOf(entry) {
    const word = entry.replace(/^[~+]+/, '').replace(/^\*+|\*+$/g, '');
    return /^[A-Za-z]+$/.test(word) ? word.toLowerCase() : null;
}

const englishPackage = readPackage(englishDirectory, '@cspell/dict-en_us');
const softwarePackage = readPackage(softwareDirectory, '@cspell/dict-software-terms');
const trieDirectory = findTrieLibrary(englishDirectory);
const triePackage = readPackage(trieDirectory, 'cspell-trie-lib');
const trieLibrary = await import(NodePath.join(trieDirectory, triePackage.exports['.'].import));

const words = new Set();

const englishTrie = trieLibrary.decodeTrie(
    NodeZlib.gunzipSync(NodeFileSystem.readFileSync(NodePath.join(englishDirectory, 'en_US.trie.gz'))),
);
for (const entry of englishTrie.words()) {
    const word = wordOf(entry);
    if (word) {
        words.add(word);
    }
}

const softwareDictionary = NodePath.join(softwareDirectory, 'dict');
const softwareFiles = NodeFileSystem.readdirSync(softwareDictionary)
    .filter((file) => file.endsWith('.txt') || file.endsWith('.txt.gz'))
    .sort();
for (const file of softwareFiles) {
    const contents = NodeFileSystem.readFileSync(NodePath.join(softwareDictionary, file));
    const text = (file.endsWith('.gz') ? NodeZlib.gunzipSync(contents) : contents).toString('utf8');
    for (const line of text.split('\n')) {
        const entry = line.trim();
        if (entry === '' || entry.startsWith('#') || entry.startsWith('!')) {
            continue;
        }
        const word = wordOf(entry);
        if (word) {
            words.add(word);
        }
    }
}

/*
 * HouseWords.txt is the house's own supplement: words the lists lack, each with its reason, reviewed like any policy
 * change. A word the lists already hold is refused, so the supplement never outlives a dictionary that caught up.
 */
const houseWordsPath = NodePath.join(policyDirectory, 'HouseWords.txt');
let houseWordCount = 0;
let previousHouseWord = '';
for (const [index, line] of NodeFileSystem.readFileSync(houseWordsPath, 'utf8').split('\n').entries()) {
    if (line === '' || line.startsWith('#')) {
        continue;
    }
    const match = /^([a-z]+) # \S.*$/.exec(line);
    if (!match) {
        throw new Error(`HouseWords.txt line ${index + 1} is not a lowercase word, a space and a # reason: ${line}`);
    }
    const word = match[1];
    if (word <= previousHouseWord) {
        throw new Error(`HouseWords.txt line ${index + 1}, ${word}, does not come after ${previousHouseWord}`);
    }
    if (words.has(word)) {
        throw new Error(`HouseWords.txt line ${index + 1}, ${word}, is in the lists already; remove it`);
    }
    previousHouseWord = word;
    words.add(word);
    houseWordCount++;
}

const englishLicense = readLicense(englishDirectory, englishPackage.name);
const softwareLicense = readLicense(softwareDirectory, softwarePackage.name);

/* Sorted by code unit, which for lowercase ASCII is the byte order Go's loader checks. */
const sorted = [...words].sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));

const header = [
    '# The words cohere-go/consistency-require-separated-folder-words splits a folder name into, one a line,',
    '# lowercase, sorted. Generated by policy/tools/generate_words/generate_words.mjs: edit nothing here.',
    `# From ${englishPackage.name} ${englishPackage.version}, under the MIT License:`,
    `#     ${englishLicense.copyright}`,
    `# From ${softwarePackage.name} ${softwarePackage.version}, under the MIT License:`,
    `#     ${softwareLicense.copyright}`,
    `# With the house's own words, ${houseWordCount} from policy/HouseWords.txt, each with its reason there.`,
    '# The license texts are in policy/Words.NOTICE.',
];
NodeFileSystem.writeFileSync(NodePath.join(policyDirectory, 'Words.txt'), [...header, ...sorted].join('\n') + '\n');

const notice = [
    'policy/Words.txt is generated from two dictionaries, each under the license below.',
    '',
    `${englishPackage.name} ${englishPackage.version}`,
    englishPackage.homepage,
    '',
    englishLicense.text,
    '',
    `${softwarePackage.name} ${softwarePackage.version}`,
    softwarePackage.homepage,
    '',
    softwareLicense.text,
];
NodeFileSystem.writeFileSync(NodePath.join(policyDirectory, 'Words.NOTICE'), notice.join('\n') + '\n');

process.stdout.write(`wrote ${sorted.length} words to policy/Words.txt\n`);
