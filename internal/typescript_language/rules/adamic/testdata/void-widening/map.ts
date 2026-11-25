const make: () => void = () => 'x';
const narrow = new Map<string, () => void>([['fn', make]]);
const wide: ReadonlyMap<string, () => number | void> = narrow;
for (const wider of wide.values()) {
const next = wider();
if (next) console.log(next.toFixed(1));
}
