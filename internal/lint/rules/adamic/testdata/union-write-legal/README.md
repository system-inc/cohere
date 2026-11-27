# Legal union writes

`delete.ts` is a delete the union-write rule leaves alone. `delete.out` is node's recorded run of it
(prints `0`), and `delete.control.out` is node's recorded run of its control, the program with
`clear(counter);` removed (prints `1`). `TestUnionWriteDeleteStaysQuiet` reads them, so the test never
starts node. `../union-write/README.md` says what a recording holds and gives the script that made them.
