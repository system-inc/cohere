/*
 * The paths a generator is given, and how a fixture records them (#sycrdr6).
 *
 * A path inside this repository, such as a public design system under testdata, is recorded relative to
 * the repository's root, which internal/testing/repository's Path reads the same way (#f598zk0), so the
 * committed file reads the same on every machine. A path outside it is recorded absolute, and Path refuses
 * that one, since cohere's tests read only this repository (#5wj23dd).
 *
 * A checkout outside this repository, which a generator may still read from, is given by its path. The
 * "<name>:<path in it>" spelling that once named such a checkout is refused with a message that says so.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodePath from 'node:path';
import * as NodeUrl from 'node:url';

const spellingPattern = /^[a-z][a-z0-9-]+:/;

/*
 * This repository's root: the nearest directory above this file whose go.mod declares the module, as
 * internal/testing/repository's Root finds it from a test's directory.
 */
const repositoryRoot = (function() {
    let directory = NodePath.dirname(NodeUrl.fileURLToPath(import.meta.url));
    for(;;) {
        const goModPath = NodePath.join(directory, 'go.mod');
        if(
            NodeFileSystem.existsSync(goModPath) &&
            /^module github\.com\/system-inc\/cohere\s*$/m.test(NodeFileSystem.readFileSync(goModPath, 'utf8'))
        ) {
            return directory;
        }
        const parent = NodePath.dirname(directory);
        if(parent === directory) {
            throw new Error(`no go.mod declaring github.com/system-inc/cohere above ${import.meta.url}`);
        }
        directory = parent;
    }
})();

/*
 * How a fixture records a path: relative to this repository when it lies inside it, with forward slashes,
 * and otherwise as the path itself.
 */
function recordPath(path) {
    const inside = NodePath.relative(repositoryRoot, path);
    if(inside === '' || inside.startsWith('..') || NodePath.isAbsolute(inside)) {
        return path;
    }
    return inside.split(NodePath.sep).join('/');
}

/*
 * Refuses a "<name>:<path>" spelling, which named a checkout outside this repository by a name the user's
 * environment mapped to its root. Nothing maps those names now, so the checkout is given by its path.
 */
function refuseSpelling(argument) {
    if(spellingPattern.test(argument)) {
        throw new Error(
            `${argument} names a checkout by a name, which nothing resolves now: give the checkout's path, ` +
                'absolute or relative to the working directory',
        );
    }
}

/*
 * The path a generator's argument names, and how a fixture records it. The argument is a path, resolved
 * against the working directory, and recorded relative to this repository when it lies inside it.
 */
export function locate(argument) {
    refuseSpelling(argument);
    const path = NodePath.resolve(argument);
    return { path, recorded: recordPath(path) };
}

/*
 * How a fixture records `path`, found inside what `argument` named: as locate records a path, once
 * `argument` is known to be a path rather than a spelling.
 */
export function recordBeside(argument, path) {
    refuseSpelling(argument);
    return recordPath(path);
}
