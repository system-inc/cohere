'use strict';

// node --test cohere-format.test.js. The save test runs the binary COHERE_BINARY names and skips
// without one, since this repository builds cohere rather than installing it.

const NodeAssert = require('node:assert/strict');
const NodeChildProcess = require('node:child_process');
const NodeFileSystem = require('node:fs');
const NodeOs = require('node:os');
const NodePath = require('node:path');
const NodeTest = require('node:test');
const { formatWithCohere, resolveCohereBinary, saveArguments } = require('./cohere-format.js');

function temporaryDirectory() {
    return NodeFileSystem.realpathSync(NodeFileSystem.mkdtempSync(NodePath.join(NodeOs.tmpdir(), 'cohere-vscode-')));
}

function writeExecutable(path) {
    NodeFileSystem.mkdirSync(NodePath.dirname(path), { recursive: true });
    NodeFileSystem.writeFileSync(path, '#!/bin/sh\n', { mode: 0o755 });
}

NodeTest.test('resolves cohere in Structure\'s order: override, project launcher, PATH', function() {
    const root = temporaryDirectory();
    const project = NodePath.join(root, 'project', 'nested');
    NodeFileSystem.mkdirSync(project, { recursive: true });
    const launcher = NodePath.join(root, 'project', 'node_modules', '.bin', 'cohere');
    const onPath = NodePath.join(root, 'bin', 'cohere');
    const override = NodePath.join(root, 'override', 'cohere');
    writeExecutable(launcher);
    writeExecutable(onPath);
    writeExecutable(override);
    const path = NodePath.join(root, 'bin');

    NodeAssert.equal(resolveCohereBinary(project, { COHERE_BINARY: override, PATH: path }), override);
    // A broken override is not a fallback to the others.
    NodeAssert.equal(resolveCohereBinary(project, { COHERE_BINARY: override + '-missing', PATH: path }), undefined);
    // The project's launcher is found from a directory under the project, ahead of PATH.
    NodeAssert.equal(resolveCohereBinary(project, { PATH: path }), launcher);
    NodeAssert.equal(resolveCohereBinary(root, { PATH: path }), onPath);
    NodeAssert.equal(resolveCohereBinary(root, { PATH: '' }), undefined);
});

NodeTest.test('a save prints what the gate writes for the same text', { skip: process.env.COHERE_BINARY === undefined }, async function() {
    const binary = process.env.COHERE_BINARY;
    const root = temporaryDirectory();
    NodeFileSystem.writeFileSync(NodePath.join(root, 'tsconfig.json'), JSON.stringify({
        compilerOptions: { strict: true, noEmit: true, module: 'esnext', moduleResolution: 'bundler', target: 'ES2022' },
        include: ['**/*.ts'],
    }));
    NodeFileSystem.writeFileSync(NodePath.join(root, 'CohereSettings.json'), '{"rules":{"no-debugger":"error"}}');
    const filePath = NodePath.join(root, 'Probe.ts');
    const onDisk = 'export const value = 1;\n';
    NodeFileSystem.writeFileSync(filePath, onDisk);
    const buffer = 'export function value(): number {\n  const   first = 1\n  debugger;\n    return first }\n';

    const saved = await formatWithCohere(binary, filePath, buffer);
    NodeAssert.equal(saved.error, undefined);
    NodeAssert.equal(NodeFileSystem.readFileSync(filePath, 'utf8'), onDisk, 'the save wrote to disk');
    NodeAssert.ok(!saved.text.includes('debugger') && saved.text.includes('const first = 1;'), saved.text);

    NodeFileSystem.writeFileSync(filePath, buffer);
    const gate = NodeChildProcess.spawnSync(binary, [...saveArguments, filePath], { cwd: root, encoding: 'utf8' });
    NodeAssert.equal(gate.status, 0, gate.stdout + gate.stderr);
    NodeAssert.equal(NodeFileSystem.readFileSync(filePath, 'utf8'), saved.text);
});

NodeTest.test('a binary that fails is an error, not a silent save', { skip: process.platform === 'win32' }, async function() {
    const root = temporaryDirectory();
    const failing = NodePath.join(root, 'cohere');
    NodeFileSystem.writeFileSync(failing, '#!/bin/sh\necho "formatter broke" >&2\nexit 1\n', { mode: 0o755 });
    const result = await formatWithCohere(failing, NodePath.join(root, 'Probe.ts'), 'x\n');
    NodeAssert.deepEqual(result, { error: 'formatter broke' });
});
