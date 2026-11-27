# Union-write probes and node's recorded runs

Each `<name>.ts` is a probe: a program tsc accepts under Adamic's options whose union write corrupts a
value, so that `probe();` throws a `TypeError` at run time. Beside it, `<name>.out` is node's recorded run
of the probe, and `<name>.control.out` is node's recorded run of its control, the same program with its one
`\nprobe();` removed, which must run clean. `TestUnionWriteRecordedProbes` reads them, so the tests never
start node. `../union-write-legal/delete.out` and `delete.control.out` are recorded the same way, the
control there being the delete program with `clear(counter);` removed.

A recording keeps only what is stable across machines, one line each:

- `source <sha256>`: the sha256 of the exact text node ran, so a program edited without being recorded
  again fails its test.
- `exit <status>`: node's exit status.
- `stdout <line>`: each line the program printed.
- `stderr <line>`: the `TypeError: ...` line node printed for an uncaught `TypeError`, without the file
  paths, source excerpt and stack node prints around it.

They were made with node v24.14.1 on macOS, by this script run from `internal/lint/rules/adamic`. Run it
again to record a new or changed probe; node runs each program from a copy named `probe.mts` so it loads
as an ES module.

```bash
scratch="$(mktemp -d)"
record() {
	cp "$1" "$scratch/probe.mts"
	node "$scratch/probe.mts" > "$scratch/stdout" 2> "$scratch/stderr"
	status=$?
	{
		echo "source $(shasum -a 256 < "$1" | cut -d' ' -f1)"
		echo "exit $status"
		sed 's/^/stdout /' "$scratch/stdout"
		grep '^TypeError: ' "$scratch/stderr" | sed 's/^/stderr /'
	} > "$2"
}
for probe in testdata/union-write/*.ts; do
	record "$probe" "${probe%.ts}.out"
	perl -0pe 's/\nprobe\(\);//' "$probe" > "$scratch/control.ts"
	record "$scratch/control.ts" "${probe%.ts}.control.out"
done
record testdata/union-write-legal/delete.ts testdata/union-write-legal/delete.out
perl -0pe 's/clear\(counter\);//' testdata/union-write-legal/delete.ts > "$scratch/control.ts"
record "$scratch/control.ts" testdata/union-write-legal/delete.control.out
```
