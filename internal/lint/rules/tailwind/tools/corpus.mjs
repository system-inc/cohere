/*
 * Paths inside a corpus, for the generators (#sycrdr6).
 *
 * A fixture that records a path inside a private checkout spells it "<corpus>:<path in it>", as in
 * "ahra:app/_theme/styles/theme.css", so the committed file reads the same on every machine and the
 * tests resolve it through internal/corpus. A generator takes the same spelling as input and records it
 * as given, so a regenerated fixture stays portable. This is internal/corpus's Locate for the Node
 * side: the corpus's variable, or else the user's corpora file, gives its root.
 *
 * A path inside this repository, such as a public design system under testdata, is recorded relative to
 * the repository's root, which internal/corpus's Resolve reads the same way (#f598zk0). Only a path
 * outside both is recorded absolute, and Resolve refuses that one.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodeOs from 'node:os';
import * as NodePath from 'node:path';
import * as NodeUrl from 'node:url';

/*
 * Each corpus's name and the variable that holds its root, a copy of internal/corpus's All, held to it
 * by that package's TestTheGeneratorsNameTheSameCorpora.
 */
const corpusVariables = {
    'ahra': 'COHERE_CORPUS_AHRA',
    'structure': 'COHERE_CORPUS_STRUCTURE',
    'connected': 'COHERE_CORPUS_CONNECTED',
    'prettier-fork': 'COHERE_PRETTIER_FORK',
};

const spellingPattern = /^([a-z][a-z0-9-]+):(.*)$/;

/*
 * This repository's root: the nearest directory above this file whose go.mod declares the module, as
 * internal/corpus's RepositoryRoot finds it from a test's directory.
 */
const repositoryRoot = (function () {
    let directory = NodePath.dirname(NodeUrl.fileURLToPath(import.meta.url));
    for (;;) {
        const goModPath = NodePath.join(directory, 'go.mod');
        if (
            NodeFileSystem.existsSync(goModPath) &&
            /^module github\.com\/system-inc\/cohere\s*$/m.test(NodeFileSystem.readFileSync(goModPath, 'utf8'))
        ) {
            return directory;
        }
        const parent = NodePath.dirname(directory);
        if (parent === directory) {
            throw new Error(`no go.mod declaring github.com/system-inc/cohere above ${import.meta.url}`);
        }
        directory = parent;
    }
})();

/*
 * How a fixture records a path that is no spelling: relative to this repository when it lies inside it,
 * with forward slashes, and otherwise as the path itself.
 */
function recordPath(path) {
    const inside = NodePath.relative(repositoryRoot, path);
    if (inside === '' || inside.startsWith('..') || NodePath.isAbsolute(inside)) {
        return path;
    }
    return inside.split(NodePath.sep).join('/');
}

function corporaFilePath() {
    return process.env.COHERE_CORPORA_CONFIG || NodePath.join(NodeOs.homedir(), '.config', 'cohere', 'corpora.json');
}

function corpusRoot(name, spelling) {
    if (!Object.hasOwn(corpusVariables, name)) {
        throw new Error(`${spelling} names the corpus "${name}", which is no corpus; the corpora are ${Object.keys(corpusVariables).join(', ')}`);
    }
    const variable = corpusVariables[name];
    if (process.env[variable]) {
        return NodePath.resolve(process.env[variable]);
    }
    const configPath = corporaFilePath();
    const config = NodeFileSystem.existsSync(configPath) ? JSON.parse(NodeFileSystem.readFileSync(configPath, 'utf8')) : {};
    if (!config[name]) {
        throw new Error(`${spelling} needs the ${name} corpus: set ${variable}, or name "${name}" in ${configPath}`);
    }
    return NodePath.resolve(config[name]);
}

/*
 * The path a generator's argument names, and how a fixture records it. A spelling is recorded as given.
 * Anything else is a path, resolved against the working directory, and recorded relative to this
 * repository when it lies inside it.
 */
export function locate(argument) {
    const match = spellingPattern.exec(argument);
    if (!match) {
        const path = NodePath.resolve(argument);
        return { path, recorded: recordPath(path) };
    }
    const [, name, inside] = match;
    return { path: NodePath.resolve(corpusRoot(name, argument), inside), recorded: argument };
}

/*
 * How a fixture records `path`, found inside what `argument` named: in the same corpus when `argument`
 * is a spelling and `path` lies inside that corpus, and otherwise as locate records a path.
 */
export function recordBeside(argument, path) {
    const match = spellingPattern.exec(argument);
    if (!match) {
        return recordPath(path);
    }
    const [, name] = match;
    const inside = NodePath.relative(corpusRoot(name, argument), path);
    if (inside.startsWith('..') || NodePath.isAbsolute(inside)) {
        return recordPath(path);
    }
    return `${name}:${inside.split(NodePath.sep).join('/')}`;
}
