'use strict';

// node --test cohere-format.test.js. The save test runs the binary COHERE_BINARY names and skips
// without one, since this repository builds cohere rather than installing it.

const NodeAssert = require('node:assert/strict');
const NodeChildProcess = require('node:child_process');
const NodeFileSystem = require('node:fs');
const NodeModule = require('node:module');
const NodeOs = require('node:os');
const NodePath = require('node:path');
const NodeTest = require('node:test');
const {
    cohereNotFoundMessage,
    environmentWithoutCohereOverride,
    formatWithCohere,
    installCommand,
    resolveCohereBinary,
    saveArguments,
    saveSettings,
} = require('./cohere-format.js');

function temporaryDirectory() {
    return NodeFileSystem.realpathSync(NodeFileSystem.mkdtempSync(NodePath.join(NodeOs.tmpdir(), 'cohere-vscode-')));
}

function writeExecutable(path) {
    NodeFileSystem.mkdirSync(NodePath.dirname(path), { recursive: true });
    NodeFileSystem.writeFileSync(path, '#!/bin/sh\n', { mode: 0o755 });
}

NodeTest.test("resolves cohere in Structure's order: override, project launcher, PATH", function() {
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

NodeTest.test(
    'a save prints what the gate writes for the same text',
    { skip: process.env.COHERE_BINARY === undefined },
    async function() {
        const binary = process.env.COHERE_BINARY;
        const root = temporaryDirectory();
        NodeFileSystem.writeFileSync(
            NodePath.join(root, 'tsconfig.json'),
            JSON.stringify({
                compilerOptions: {
                    strict: true,
                    noEmit: true,
                    module: 'esnext',
                    moduleResolution: 'bundler',
                    target: 'ES2022',
                },
                include: ['**/*.ts'],
            }),
        );
        // The two-file setup the README gives: cohere refuses to format under settings with no Nexus tier.
        NodeFileSystem.writeFileSync(
            NodePath.join(root, 'CohereSettings.json'),
            '{"extends":"./NexusCohereSettings.json","rules":{"no-debugger":"error"}}',
        );
        NodeFileSystem.writeFileSync(
            NodePath.join(root, 'NexusCohereSettings.json'),
            '{"format":{"tabWidth":4,"singleQuote":true,"printWidth":120}}',
        );
        const filePath = NodePath.join(root, 'Probe.ts');
        const onDisk = 'export const value = 1;\n';
        NodeFileSystem.writeFileSync(filePath, onDisk);
        const buffer = 'export function value(): number {\n  const   first = 1\n  debugger;\n    return first }\n';

        const saved = await formatWithCohere(binary, filePath, buffer);
        NodeAssert.equal(saved.error, undefined);
        NodeAssert.equal(NodeFileSystem.readFileSync(filePath, 'utf8'), onDisk, 'the save wrote to disk');
        NodeAssert.ok(!saved.text.includes('debugger') && saved.text.includes('const first = 1;'), saved.text);

        NodeFileSystem.writeFileSync(filePath, buffer);
        // Without the override, as the save runs it: a dispatcher handed COHERE_BINARY naming itself execs itself
        // forever, which hung this test before the environment was stripped here too.
        const gate = NodeChildProcess.spawnSync(binary, [...saveArguments, filePath], {
            cwd: root,
            encoding: 'utf8',
            env: environmentWithoutCohereOverride(),
            timeout: 600000,
        });
        NodeAssert.equal(gate.status, 0, gate.stdout + gate.stderr);
        NodeAssert.equal(NodeFileSystem.readFileSync(filePath, 'utf8'), saved.text);
    },
);

NodeTest.test(
    'a binary that fails is an error, not a silent save',
    { skip: process.platform === 'win32' },
    async function() {
        const root = temporaryDirectory();
        const failing = NodePath.join(root, 'cohere');
        NodeFileSystem.writeFileSync(failing, '#!/bin/sh\necho "formatter broke" >&2\nexit 1\n', { mode: 0o755 });
        const result = await formatWithCohere(failing, NodePath.join(root, 'Probe.ts'), 'x\n');
        NodeAssert.deepEqual(result, { error: 'formatter broke' });
    },
);

NodeTest.test('the formatter is registered for what cohere formats through stdin, and not for Go', function() {
    // extension.js activated against a stand-in for the vscode module, which only VS Code provides, to
    // read the document selector it registers its formatter with.
    const selectors = [];
    const vscode = {
        languages: {
            registerDocumentFormattingEditProvider(selector) {
                selectors.push(selector);
                return { dispose() {} };
            },
        },
    };
    const load = NodeModule._load;
    NodeModule._load = function(request, ...rest) {
        return request === 'vscode' ? vscode : load.call(this, request, ...rest);
    };
    try {
        require('./extension.js').activate({ subscriptions: [] });
    }
    finally {
        NodeModule._load = load;
    }

    NodeAssert.equal(selectors.length, 1);
    const selector = selectors[0];
    NodeAssert.equal(selector.scheme, 'file');
    function selected(filePath) {
        return typeof selector.pattern === 'string' && NodePath.matchesGlob(filePath, selector.pattern);
    }
    for(const extension of saveSettings.extensions) {
        NodeAssert.ok(selected(`/work/project/Probe${extension}`), `${extension} is not formatted on save`);
    }
    for(const fileName of ['main.go', 'go.mod', 'Package.swift', 'Notes.txt']) {
        NodeAssert.ok(!selected(`/work/project/${fileName}`), `${fileName} is handed to cohere on save`);
    }
    // Go is not in the list until cohere formats it through stdin (#qtna8m4).
    NodeAssert.ok(!saveSettings.extensions.includes('.go'));
});

NodeTest.test(
    'a project cohere has no stdin path for saves as typed, and other refusals are errors',
    { skip: process.platform === 'win32' },
    async function() {
        const root = temporaryDirectory();
        const refusing = NodePath.join(root, 'cohere');
        // The sentence cohere's front door prints for a file in a Go module, as the release build says it.
        NodeFileSystem.writeFileSync(
            refusing,
            '#!/bin/sh\ncat > /dev/null\necho "cohere: --stdin-filepath is not implemented for Go yet, so the run was refused rather than run without it, and nothing was checked." >&2\nexit 1\n',
            { mode: 0o755 },
        );
        const buffer = 'export const value   = 1\n';
        NodeAssert.deepEqual(await formatWithCohere(refusing, NodePath.join(root, 'Probe.ts'), buffer), {
            text: buffer,
        });

        // Any other flag refused is not a decline: it says the save's own arguments are wrong.
        const refusingFix = NodePath.join(root, 'cohere-fix');
        NodeFileSystem.writeFileSync(
            refusingFix,
            '#!/bin/sh\ncat > /dev/null\necho "cohere: --fix is not implemented for Go yet" >&2\nexit 1\n',
            { mode: 0o755 },
        );
        NodeAssert.deepEqual(await formatWithCohere(refusingFix, NodePath.join(root, 'Probe.ts'), buffer), {
            error: 'cohere: --fix is not implemented for Go yet',
        });
    },
);

NodeTest.test(
    'a save in a Go module leaves the buffer as typed, with no error',
    { skip: process.env.COHERE_BINARY === undefined },
    async function() {
        const binary = process.env.COHERE_BINARY;
        const root = temporaryDirectory();
        NodeFileSystem.writeFileSync(NodePath.join(root, 'go.mod'), 'module example.com/probe\n\ngo 1.22\n');
        for(const [fileName, buffer] of [
            ['main.go', 'package probe\nfunc  Value( ) int { return 1 }\n'],
            ['Probe.ts', 'export const value   = 1\n'],
        ]) {
            const saved = await formatWithCohere(binary, NodePath.join(root, fileName), buffer);
            NodeAssert.deepEqual(saved, { text: buffer }, fileName);
        }
    },
);

NodeTest.test('not finding cohere says so, where it looked, and how to install it', function() {
    const message = cohereNotFoundMessage('/work/project', { PATH: '/usr/bin' });
    NodeAssert.ok(message.includes('was not formatted'), message);
    NodeAssert.ok(message.includes('/work/project'), message);
    NodeAssert.ok(message.includes(installCommand), message);
    // A broken override names itself, since nothing else was looked at.
    const overridden = cohereNotFoundMessage('/work/project', { COHERE_BINARY: '/nowhere/cohere' });
    NodeAssert.ok(overridden.includes('COHERE_BINARY names /nowhere/cohere'), overridden);
});

NodeTest.test(
    'cohere runs without the override, which would make a dispatcher exec itself',
    { skip: process.platform === 'win32' },
    async function() {
        NodeAssert.equal(
            environmentWithoutCohereOverride({ COHERE_BINARY: '/x', PATH: '/bin' }).COHERE_BINARY,
            undefined,
        );

        // End to end: a stand-in cohere that reports whether it inherited the override.
        const root = temporaryDirectory();
        const binary = NodePath.join(root, 'cohere');
        NodeFileSystem.writeFileSync(
            binary,
            '#!/bin/sh\ncat > /dev/null\nprintf "override=%s" "${COHERE_BINARY-unset}"\n',
            { mode: 0o755 },
        );
        const saved = process.env.COHERE_BINARY;
        process.env.COHERE_BINARY = binary;
        try {
            const result = await formatWithCohere(binary, NodePath.join(root, 'Probe.ts'), 'x\n');
            NodeAssert.deepEqual(result, { text: 'override=unset' });
        }
        finally {
            if(saved === undefined) {
                delete process.env.COHERE_BINARY;
            }
            else {
                process.env.COHERE_BINARY = saved;
            }
        }
    },
);

NodeTest.test("on Windows the project's cohere is its platform package's cohere.exe, under pnpm and npm", function() {
    const windows = { platform: 'win32', arch: 'x64' };
    function writeFile(path, contents) {
        NodeFileSystem.mkdirSync(NodePath.dirname(path), { recursive: true });
        NodeFileSystem.writeFileSync(path, contents);
    }

    // pnpm: the project's node_modules/@system-inc/cohere is a link into .pnpm, and the platform package
    // sits beside cohere's real directory there, not beside the link.
    const pnpm = temporaryDirectory();
    const store = NodePath.join(
        pnpm,
        'node_modules',
        '.pnpm',
        '@system-inc+cohere@1.0.0',
        'node_modules',
        '@system-inc',
    );
    writeFile(NodePath.join(store, 'cohere', 'package.json'), '{"name":"@system-inc/cohere"}');
    writeFile(NodePath.join(store, 'cohere-win32-x64', 'package.json'), '{"name":"@system-inc/cohere-win32-x64"}');
    writeFile(NodePath.join(store, 'cohere-win32-x64', 'bin', 'cohere.exe'), '');
    NodeFileSystem.mkdirSync(NodePath.join(pnpm, 'node_modules', '@system-inc'), { recursive: true });
    NodeFileSystem.symlinkSync(
        NodePath.join(store, 'cohere'),
        NodePath.join(pnpm, 'node_modules', '@system-inc', 'cohere'),
    );
    // The shim a package manager writes on Windows, which can't be spawned without a shell.
    writeFile(NodePath.join(pnpm, 'node_modules', '.bin', 'cohere.cmd'), '@node launcher.js %*\r\n');
    const nested = NodePath.join(pnpm, 'app');
    NodeFileSystem.mkdirSync(nested);
    NodeAssert.equal(
        resolveCohereBinary(nested, { PATH: '' }, windows),
        NodePath.join(store, 'cohere-win32-x64', 'bin', 'cohere.exe'),
    );

    // npm: the two packages are siblings in the project's node_modules.
    const npm = temporaryDirectory();
    writeFile(
        NodePath.join(npm, 'node_modules', '@system-inc', 'cohere', 'package.json'),
        '{"name":"@system-inc/cohere"}',
    );
    writeFile(
        NodePath.join(npm, 'node_modules', '@system-inc', 'cohere-win32-x64', 'package.json'),
        '{"name":"@system-inc/cohere-win32-x64"}',
    );
    const npmBinary = NodePath.join(npm, 'node_modules', '@system-inc', 'cohere-win32-x64', 'bin', 'cohere.exe');
    writeFile(npmBinary, '');
    NodeAssert.equal(resolveCohereBinary(npm, { PATH: '' }, windows), npmBinary);

    // Installed for another architecture only: not this machine's cohere, and nothing else is found.
    NodeAssert.equal(resolveCohereBinary(npm, { PATH: '' }, { platform: 'win32', arch: 'arm64' }), undefined);
});
