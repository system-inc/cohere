/*
 * Ask the real Tailwind engine which variants the framework registers, and under which order
 * numbers.
 *
 * This is the static half of the variant component, and it is the half `gen_tailwind_variant`
 * deliberately does not print. That tool captures the *behaviour* — `getVariantOrder` and
 * `getClassOrder` over a corpus — into a fixture a test reads. This one captures the *registry*
 * into shipped Go, because `LoadDesignSystem` needs it at lint time and a fixture under `testdata`
 * is not shipped.
 *
 * # Why the registry can be a table when the order map cannot
 *
 * `variant.go` argues at length that no static table can hold a variant's *position*, and it is
 * right: `getVariantOrder` assigns a dense index per run over only the variants that were parsed,
 * so `hover` is 0 in one run and 1 in another. Nothing here contradicts that. What is captured is
 * the *registration* — the name, the `order` number `Variants.set` assigned, and the kind — which
 * is a fact about the installed framework and does not vary per run or per repository. The dense
 * index is still computed per class list, from these registrations, by `BuildVariantOrder`.
 *
 * The distinction is the same one `CollapseFamilies` rests on: a fact about roots is a table, a
 * computation per class is code.
 *
 * # What is captured, and what cannot be
 *
 * `entries` is every registered root with its order and kind. `compareFnOrders` is which order
 * numbers carry a comparison function. The function itself cannot cross this boundary — it is a
 * closure over the design system's theme — so the Go side registers `CompareBreakpoints` against
 * those orders, reading the *live* theme. That is not a shortcut. Redefining `--breakpoint-sm` to
 * `200rem` moves `sm` from index 0 to index 4 on the engine, measured, so a comparison that read a
 * hardcoded scale would be wrong on exactly the repository that customized its breakpoints.
 *
 * The directions are captured rather than assumed: this script probes each grouped order with two
 * of its own members and reports whether the engine ordered them ascending or descending, so a
 * Tailwind release that flipped one produces a diff rather than a silent reversal.
 *
 * `@plugin` and `@config` are stubbed, matching the sibling generators. A JavaScript plugin can
 * call `addVariant`, so a repository with one registers a root this enumeration does not see. That
 * is a stated narrowing: this table is the *framework's* registrations, and a repository's own
 * contributions stay live-loaded through `@custom-variant`.
 *
 * Node is required to run this and never to use its result.
 */

import * as NodeFileSystem from 'node:fs';
import * as NodePath from 'node:path';

const [, , packageRootArgument] = process.argv;
if (!packageRootArgument) {
    process.stderr.write('usage: enumerate.mjs <tailwindcss package root>\n');
    process.exit(2);
}

const packageRoot = NodePath.resolve(packageRootArgument);
const packageJson = JSON.parse(NodeFileSystem.readFileSync(NodePath.join(packageRoot, 'package.json'), 'utf8'));
const bundle = await import(NodePath.join(packageRoot, 'dist', 'lib.mjs'));

/* Resolve `@import` the way a bundler would, matching every sibling generator's loader exactly. */
function loadStylesheet(identifier, base) {
    let resolved;
    if (identifier === 'tailwindcss') {
        resolved = NodePath.join(packageRoot, 'index.css');
    } else if (identifier.startsWith('tailwindcss/')) {
        resolved = NodePath.join(packageRoot, identifier.slice('tailwindcss/'.length));
    } else {
        resolved = NodePath.resolve(base, identifier);
    }
    if (!resolved.endsWith('.css')) resolved += '.css';
    return { base: NodePath.dirname(resolved), content: NodeFileSystem.readFileSync(resolved, 'utf8') };
}

function loadModule(identifier, base) {
    return { base, path: identifier, module: {} };
}

async function loadDesignSystem(css) {
    return await bundle.__unstable__loadDesignSystem(css, {
        base: '/',
        loadStylesheet: async (identifier, importBase) => loadStylesheet(identifier, importBase),
        loadModule: async (identifier, importBase) => loadModule(identifier, importBase),
    });
}

const designSystem = await loadDesignSystem('@import "tailwindcss";');

const entries = [];
for (const [name, info] of designSystem.variants.entries()) {
    entries.push({ name, order: info.order, kind: info.kind });
}

const compareFnOrders = Array.from(designSystem.variants.compareFns.keys()).sort((a, z) => a - z);

/*
 * Probe each grouped order's direction, rather than reading it off `variants.ts`.
 *
 * Two members of the group are parsed into a fresh design system and their dense indices read back.
 * Whichever resolves to the smaller breakpoint taking the smaller index means ascending. A release
 * that flipped `max` to ascending would change this field and produce a diff in the generated file,
 * which is the whole reason the generator exists.
 *
 * The probe pair is chosen from the group's own members: a static member takes its root as the
 * probe, a functional one takes `<root>-sm` and `<root>-lg`, which are the names the default scale
 * defines. A group whose members yield no usable pair reports `null` and the Go side refuses to
 * render, rather than guessing a direction.
 */
function probePairFor(members) {
    const statics = members.filter((member) => member.kind === 'static').map((member) => member.name);
    if (statics.includes('sm') && statics.includes('lg')) return ['sm', 'lg'];

    const functional = members.filter((member) => member.kind === 'functional').map((member) => member.name);
    for (const root of functional) {
        // `@` is written `@sm`, everything else `min-sm`.
        const small = root === '@' ? '@sm' : `${root}-sm`;
        const large = root === '@' ? '@lg' : `${root}-lg`;
        return [small, large];
    }
    return null;
}

const byOrder = new Map();
for (const entry of entries) {
    if (!byOrder.has(entry.order)) byOrder.set(entry.order, []);
    byOrder.get(entry.order).push(entry);
}

const comparisons = [];
for (const order of compareFnOrders) {
    const members = byOrder.get(order) ?? [];
    const pair = probePairFor(members);
    if (pair === null) {
        comparisons.push({ order, ascending: null, probe: null, members: members.map((m) => m.name) });
        continue;
    }

    // A fresh design system per probe, because the dense index is a function of exactly which
    // variants have been parsed. Reusing one would let an earlier probe widen a later one's map.
    const probeSystem = await loadDesignSystem('@import "tailwindcss";');
    for (const raw of pair) probeSystem.parseVariant(raw);
    const probeOrder = probeSystem.getVariantOrder();
    const [smallRaw, largeRaw] = pair;
    const smallIndex = probeOrder.get(probeSystem.parseVariant(smallRaw)) ?? null;
    const largeIndex = probeOrder.get(probeSystem.parseVariant(largeRaw)) ?? null;

    comparisons.push({
        order,
        ascending: smallIndex === null || largeIndex === null ? null : smallIndex < largeIndex,
        probe: { small: smallRaw, smallIndex, large: largeRaw, largeIndex },
        members: members.map((m) => m.name),
    });
}

process.stdout.write(
    JSON.stringify({ tailwindVersion: packageJson.version, entries, compareFnOrders, comparisons }, null, 4) + '\n',
);
