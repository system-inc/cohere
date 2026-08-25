/*
 * Design-system loader, shared by the descriptor extractor and its harness.
 *
 * Lifted from `tools/gen_tailwind_collapse/enumerate.mjs` lines 1-108, which is proven against two
 * repos: it walks the `@import` graph through `loadStylesheet` and takes the ESM branch of
 * Tailwind's exports map, because the `require` branch resolves to `dist/lib.js`, which does not
 * export `__unstable__loadDesignSystem` at all.
 *
 * It additionally exposes `inferDataType`, Tailwind's own type predicate, read out of the installed
 * package rather than transcribed. That matters for what this tool is testing: the claim under test
 * is that a root's reading is a function of the inferred data type, and a transcribed predicate
 * would be testing the transcription instead of the claim.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodeModule from 'node:module';
import * as NodePath from 'node:path';
import * as NodeUrl from 'node:url';

/*
 * Resolve Tailwind's ESM entry.
 *
 * `require.resolve` takes the `require` branch of the exports map, and for Tailwind that is
 * `dist/lib.js`, which does not export `__unstable__loadDesignSystem` at all. Only the ESM build
 * does, so the package root is located through its `package.json` and the `import` condition read
 * directly.
 */
function resolveModuleUrl(specifier, directory) {
    const requireFromDirectory = NodeModule.createRequire(NodePath.join(directory, 'noop.js'));
    const packageJsonPath = requireFromDirectory.resolve(specifier + '/package.json');
    const packageJson = JSON.parse(NodeFileSystem.readFileSync(packageJsonPath, 'utf8'));
    const rootExport = packageJson.exports?.['.'] ?? packageJson.exports;
    const importEntry = typeof rootExport === 'string' ? rootExport : (rootExport?.import ?? rootExport?.default);
    const resolvedPath = importEntry
        ? NodePath.join(NodePath.dirname(packageJsonPath), importEntry)
        : requireFromDirectory.resolve(specifier);
    return NodeUrl.pathToFileURL(resolvedPath).href;
}

function resolveStylesheet(specifier, directory) {
    if (specifier.startsWith('.') || NodePath.isAbsolute(specifier)) {
        return NodePath.resolve(directory, specifier);
    }
    const requireFromDirectory = NodeModule.createRequire(NodePath.join(directory, 'noop.js'));
    if (specifier.endsWith('.css')) return requireFromDirectory.resolve(specifier);

    const packageJsonPath = requireFromDirectory.resolve(specifier + '/package.json');
    const packageJson = JSON.parse(NodeFileSystem.readFileSync(packageJsonPath, 'utf8'));
    const rootExport = packageJson.exports?.['.'] ?? packageJson.exports;
    const styleEntry = (typeof rootExport === 'object' ? rootExport?.style : undefined) ?? packageJson.style;
    if (!styleEntry) return requireFromDirectory.resolve(specifier);
    return NodePath.join(NodePath.dirname(packageJsonPath), styleEntry);
}

/*
 * `inferDataType`, recovered from the installed bundle.
 *
 * It is not on the design system and it is not a package export, so it is reached through the chunk
 * that carries it. The chunk filename is content-hashed and changes every release, so the chunk is
 * found by its export signature rather than by name: `inferDataType` is minified to `ge` and
 * re-exported as `d`, alongside nine siblings. Probing candidate chunks for a function that types
 * `url(a.png)` as `image` and `3px` as `length` identifies it without depending on the hash.
 *
 * If a future Tailwind reshapes the chunk, this returns null and the caller reports the descriptor
 * model as untested rather than silently falling back to a hand-rolled predicate, which would test
 * the fallback instead of the engine.
 */
async function loadInferDataType(tailwindModuleUrl) {
    const distDirectory = NodePath.dirname(NodeUrl.fileURLToPath(tailwindModuleUrl));
    let chunkNames;
    try {
        chunkNames = NodeFileSystem.readdirSync(distDirectory).filter((name) => name.endsWith('.mjs'));
    }
    catch {
        return null;
    }

    for (const chunkName of chunkNames) {
        let chunkModule;
        try {
            chunkModule = await import(NodeUrl.pathToFileURL(NodePath.join(distDirectory, chunkName)).href);
        }
        catch {
            continue;
        }
        for (const exported of Object.values(chunkModule)) {
            if (typeof exported !== 'function' || exported.length !== 2) continue;
            try {
                const probeTypes = ['image', 'length', 'percentage', 'color'];
                if (exported('url(a.png)', probeTypes) !== 'image') continue;
                if (exported('3px', probeTypes) !== 'length') continue;
                if (exported('50%', probeTypes) !== 'percentage') continue;
                if (exported('red', probeTypes) !== 'color') continue;
                if (exported('var(--a)', probeTypes) !== null) continue;
                return exported;
            }
            catch {
                continue;
            }
        }
    }
    return null;
}

export async function loadDesignSystem(entryPointArgument) {
    const entryPointPath = NodePath.resolve(entryPointArgument);
    const entryDirectory = NodePath.dirname(entryPointPath);
    const tailwindModuleUrl = resolveModuleUrl('tailwindcss', entryDirectory);
    const tailwindModule = await import(tailwindModuleUrl);

    const designSystem = await tailwindModule.__unstable__loadDesignSystem(
        NodeFileSystem.readFileSync(entryPointPath, 'utf8'),
        {
            base: entryDirectory,
            loadModule: async function (specifier, base, resourceType) {
                try {
                    const requireFromBase = NodeModule.createRequire(NodePath.join(base, 'noop.js'));
                    const resolved = requireFromBase.resolve(specifier);
                    const loaded = await import(NodeUrl.pathToFileURL(resolved).href);
                    return { base: NodePath.dirname(resolved), module: loaded.default ?? loaded };
                }
                catch {
                    return { base, module: resourceType === 'config' ? {} : () => {} };
                }
            },
            loadStylesheet: async function (specifier, base) {
                try {
                    const resolved = resolveStylesheet(specifier, base);
                    return { base: NodePath.dirname(resolved), content: NodeFileSystem.readFileSync(resolved, 'utf8') };
                }
                catch {
                    return { base: '', content: '' };
                }
            },
        },
    );

    const requireFromEntry = NodeModule.createRequire(NodePath.join(entryDirectory, 'noop.js'));
    const tailwindVersion = JSON.parse(
        NodeFileSystem.readFileSync(requireFromEntry.resolve('tailwindcss/package.json'), 'utf8'),
    ).version;

    return {
        designSystem,
        tailwindVersion,
        entryPoint: entryPointPath,
        inferDataType: await loadInferDataType(tailwindModuleUrl),
    };
}

/*
 * The reading of a class, as the engine computes it.
 *
 * `compileAstNodes` is an internal API and upstream may rename it. It is used here rather than the
 * public `getClassOrder` because the question is diagnostic: `getClassOrder` returns a position
 * within the queried set, which answers "did these two classes agree" but not "what did this class
 * declare". `{order, count}` is the thing the Go descriptor table has to reproduce, so it is the
 * thing that has to be measured. `getClassOrder` is used separately as the end-to-end control.
 *
 * `nodes[0]` is deliberate. A candidate can parse several ways (`border-b` parses two), and
 * `getClassOrder` takes the position of the first (`sort.ts:22`). Taking the first here matches it.
 *
 * A null return means the class produced no reading at all. That is not agreement and callers must
 * not count it as one: in the ahra corpus 10 of 1,208 distinct classes return null, and a harness
 * that scored them as passes would be blind on exactly the classes most likely to break.
 */
export function readingOf(designSystem, className) {
    let candidate;
    try {
        candidate = Array.from(designSystem.parseCandidate?.(className) ?? [])[0];
    }
    catch {
        return null;
    }
    if (!candidate) return null;

    let nodes;
    try {
        nodes = designSystem.compileAstNodes(candidate);
    }
    catch {
        return null;
    }
    if (!nodes || nodes.length === 0) return null;

    const propertySort = nodes[0]?.propertySort;
    if (!propertySort) return null;
    return { order: propertySort.order ?? [], count: propertySort.count ?? 0 };
}

export function parseCandidate(designSystem, className) {
    try {
        return Array.from(designSystem.parseCandidate?.(className) ?? [])[0] ?? null;
    }
    catch {
        return null;
    }
}

export function readingKey(reading) {
    if (reading === null) return 'null';
    return '[' + reading.order.join(',') + ']#' + reading.count;
}
