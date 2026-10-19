# Ambient declaration probes (#4emavvv)

With tsc 6.0.3 and Node v24.19.0, all fourteen main.ts files compile together under
`--strict --target es2024 --module nodenext --lib es2024`. Each is an ES module so their
independent declarations cannot fill another probe's missing binding.

From cohere, with tsc installed at `$TSC` and an output directory `$PROBE_OUT`:

```sh
node "$TSC" --ignoreConfig --strict --target es2024 --module nodenext --lib es2024 \
  --rootDir internal/lint/rules/adamic/testdata/ambient-declarations \
  --outDir "$PROBE_OUT" internal/lint/rules/adamic/testdata/ambient-declarations/*/main.ts
printf '{"type":"module"}\n' > "$PROBE_OUT/package.json"
```

Run each emitted `<form>/main.js` with Node. const, let, var, function, class,
global-value, reference, reference-types, reference-lib, reference-library-name and reference-transitive throw ReferenceError; global-method and module throw
TypeError. `field/main.js` exits 0 and prints `2`: the base class defines x, and
the derived `declare readonly x: number` is a field annotation, not a new binding.
This rule does not judge field annotation refinements or initialization promises.

A path, types or lib reference is reported when a value from its resolved declaration-file closure is read. Transitive references are followed through the compiler's resolution results.
An unused directive and a type-only use do not establish a missing runtime value
and stay quiet. Declaration files themselves are outside this source-file rule.
The direct path reference is read twice but is reported once.

A local file named `lib.es2024.d.ts` is still an ordinary declaration file.
Only the compiler's actual standard libraries are excluded as configured libraries.

The const fixture deliberately includes U+FEFF between `const` and `limit`. It is
JavaScript whitespace; diagnostic names must trim it with the JavaScript helper,
not Go's strings.TrimSpace. The quoted-name assertion catches the old behaviour.
