'use strict';

// The save, without VS Code: find cohere the way the Structure CLI does, hand it the buffer, and read
// back what `cohere --fix --format` writes for the file. Kept apart from extension.js so it can be run
// and tested with plain node (cohere-format.test.js).

const NodeChildProcess = require('node:child_process');
const NodeFileSystem = require('node:fs');
const NodePath = require('node:path');

const cohereBinaryFileName = process.platform === 'win32' ? 'cohere.exe' : 'cohere';

// The arguments a save runs with: the gate's own, with no engine named, so a save formats with whatever
// cohere's default is.
const saveArguments = ['--fix', '--format'];

/*
 * Finds the cohere binary for a project, or returns undefined having found nothing.
 *
 * The order is Structure's (libraries/structure/command-line/Structure.ts, resolveCohereBinary), so
 * the editor and `s c` run the same binary:
 *
 *   1. COHERE_BINARY, an absolute path, which is the cohere dispatcher's own override. A broken one is
 *      a failure rather than a fallback, because whoever set it named the binary they want.
 *   2. The project's own launcher, node_modules/.bin/cohere, at the project root or above it.
 *   3. A cohere on PATH.
 */
function resolveCohereBinary(projectDirectory, environment = process.env) {
    const overridePath = environment.COHERE_BINARY;
    if(overridePath !== undefined && overridePath !== '') {
        return NodeFileSystem.existsSync(overridePath) ? overridePath : undefined;
    }

    for(let directory = projectDirectory; ; directory = NodePath.dirname(directory)) {
        const candidate = NodePath.join(directory, 'node_modules', '.bin', cohereBinaryFileName);
        if(NodeFileSystem.existsSync(candidate)) {
            return candidate;
        }
        if(NodePath.dirname(directory) === directory) {
            break;
        }
    }

    for(const pathDirectory of (environment.PATH ?? '').split(NodePath.delimiter)) {
        if(pathDirectory === '') {
            continue;
        }
        const candidate = NodePath.resolve(pathDirectory, cohereBinaryFileName);
        if(NodeFileSystem.existsSync(candidate)) {
            return candidate;
        }
    }
    return undefined;
}

/*
 * Runs one save: `cohere --fix --format --stdin-filepath <file>` with the buffer on stdin.
 *
 * Resolves to { text } on success, which is the buffer unchanged when cohere declines the file (no
 * printer for its type, outside the format scope, source that does not parse), or to { error } when
 * cohere failed: a broken formatter, a project it cannot load, a binary that would not start. The
 * caller shows the error and saves the buffer as typed.
 *
 * It runs from the file's own directory, so cohere finds the nearest project the way `s c` run there
 * would, nested repositories included.
 */
function formatWithCohere(binary, filePath, text, cancellation) {
    return new Promise(function(resolve) {
        const child = NodeChildProcess.spawn(binary, [...saveArguments, '--stdin-filepath', filePath], {
            cwd: NodePath.dirname(filePath),
        });
        const stdout = [];
        const stderr = [];
        child.stdout.on('data', function(chunk) {
            stdout.push(chunk);
        });
        child.stderr.on('data', function(chunk) {
            stderr.push(chunk);
        });
        child.on('error', function(error) {
            resolve({ error: `could not run ${binary}: ${error.message}` });
        });
        child.on('close', function(code, signal) {
            if(signal !== null) {
                resolve({ error: `cohere stopped on ${signal}` });
                return;
            }
            if(code !== 0) {
                resolve({ error: Buffer.concat(stderr).toString('utf8').trim() || `cohere exited ${code}` });
                return;
            }
            resolve({ text: Buffer.concat(stdout).toString('utf8') });
        });
        if(cancellation !== undefined) {
            cancellation(function() {
                child.kill();
            });
        }
        child.stdin.on('error', function() {
            // A child that exits before reading all of stdin closes the pipe; its exit says why.
        });
        child.stdin.end(text, 'utf8');
    });
}

module.exports = { formatWithCohere, resolveCohereBinary, saveArguments };
