package release

import (
	"strings"
)

// DispatcherLauncher returns the Node script published as the `verify` command.
//
// The launcher is Node rather than Go, which is a deliberate reversal of the obvious design. A Go
// dispatcher would have to be cross-compiled per platform, which makes it a seventh platform
// package rather than one package everyone installs — and the whole point of the dispatcher package
// is that a consumer runs one `pnpm add verify` and gets the right binary. Node is already present
// by construction (this is an npm install), and `require.resolve` asks the package manager where a
// package actually is, which is more reliable than any filesystem walk: it understands pnpm's
// isolated store, npm's hoisting, and yarn's layouts without having to model any of them.
//
// The script embeds here rather than living as a checked-in `.js` file so that the platform names,
// the scope, and the binary names have exactly one definition. A launcher whose idea of the package
// name drifted from the manifest's would resolve nothing on every machine, and that failure looks
// identical to an unsupported platform.
func DispatcherLauncher() string {
	replacements := []string{
		"__SCOPE__", PlatformPackageScope,
		"__OVERRIDE_VARIABLE__", BinaryOverrideVariable,
	}
	return strings.NewReplacer(replacements...).Replace(launcherSource)
}

// launcherSource is the dispatcher script, with placeholders filled in by DispatcherLauncher.
//
// It is CommonJS rather than an ES module because a `bin` script has no package.json of its own to
// declare a type, and a `.js` file inside a package without `"type": "module"` is CommonJS. Writing
// it as ESM would work until someone added a type field, which is a trap rather than a design.
const launcherSource = `#!/usr/bin/env node
'use strict';

// The verify launcher. It finds the binary for this platform and hands the process over to it.
//
// The one rule this file exists to hold: a missing binary exits non-zero naming the platform. It
// never falls through to a bare command name, never resolves a binary built for another platform,
// and never exits zero having run nothing. The gate verify replaces printed a green checkmark over
// zero files for days because a resolver did exactly that, and an empty result is indistinguishable
// from a clean tree.

const childProcess = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const scope = '__SCOPE__';
const overrideVariable = '__OVERRIDE_VARIABLE__';

// Node's process.arch and process.platform are already npm's spelling, which is the same spelling
// the platform packages publish under. No translation belongs here.
const platformPackage = scope + '/' + process.platform + '-' + process.arch;
const binaryFileName = process.platform === 'win32' ? 'verify.exe' : 'verify';

function fail(message) {
    process.stderr.write('verify: ' + message + '\n');
    process.exit(1);
}

// resolveOverride validates an explicitly requested binary.
//
// Every failure is fatal rather than a fallback. Someone who sets this has stated which binary they
// want to run, so quietly running a different one would hand them results they would read as their
// own build's. A stale export in a shell profile is exactly how that happens.
function resolveOverride(requested) {
    const resolved = path.resolve(requested);
    let information;
    try {
        information = fs.statSync(resolved);
    } catch {
        fail(overrideVariable + ' points at ' + resolved + ', and there is nothing there.');
    }
    if (information.isDirectory()) {
        fail(overrideVariable + ' points at ' + resolved + ', which is a directory rather than a binary.');
    }
    return resolved;
}

// resolvePlatformBinary asks the package manager where the platform package is.
//
// require.resolve is used rather than a walk up node_modules because it is the package manager's
// own answer: it understands pnpm's isolated store, npm's hoisting, and yarn's layouts without this
// file having to model any of them. The package.json is resolved rather than the package itself
// because the platform packages have no entry point — they are a binary and a manifest.
function resolvePlatformBinary() {
    let manifestPath;
    try {
        manifestPath = require.resolve(platformPackage + '/package.json');
    } catch {
        return null;
    }
    return path.join(path.dirname(manifestPath), 'bin', binaryFileName);
}

function resolveBinary() {
    const requested = (process.env[overrideVariable] || '').trim();
    if (requested) {
        return resolveOverride(requested);
    }

    const candidate = resolvePlatformBinary();
    if (candidate === null) {
        fail(
            'no binary for ' + process.platform + '/' + process.arch + '.\n' +
            '  ' + platformPackage + ' is not installed.\n' +
            '  If your package manager skipped optional dependencies, reinstall without that flag.\n' +
            '  To point at a local build instead, set ' + overrideVariable + '.',
        );
    }

    if (!fs.existsSync(candidate)) {
        // The package resolved but its binary is absent. This is a packaging failure rather than an
        // unsupported platform, and it is named separately so nobody goes hunting the wrong bug.
        fail(
            platformPackage + ' is installed but its binary is missing at ' + candidate + '.\n' +
            '  The package is incomplete — reinstall it.',
        );
    }

    return candidate;
}

const binary = resolveBinary();
const forwarded = process.argv.slice(2);

// Replace this process where the platform allows it, so the binary inherits the terminal directly
// and its exit status is the one the caller sees, with no layer in between able to lose it. Losing
// a non-zero exit is how a gate goes quietly green, which is the failure this whole tool exists to
// make impossible.
if (process.platform !== 'win32' && typeof process.execve === 'function') {
    try {
        process.execve(binary, [binary, ...forwarded], process.env);
    } catch {
        // Older Node, or a platform that will not execve. The spawn below is the real path.
    }
}

const result = childProcess.spawnSync(binary, forwarded, { stdio: 'inherit' });

if (result.error) {
    fail('could not run ' + binary + ': ' + result.error.message);
}

// A process killed by a signal reports a null status. Exiting zero there would report success for a
// run that was terminated, so the conventional 128 + signal encoding is used instead. This is the
// case the obvious implementation gets wrong, because null is falsy and reads as "fine".
if (result.status === null) {
    const signalNumber = require('node:os').constants.signals[result.signal] || 0;
    process.exit(signalNumber ? 128 + signalNumber : 1);
}

process.exit(result.status);
`
