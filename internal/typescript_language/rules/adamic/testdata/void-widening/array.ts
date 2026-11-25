const make: () => void = () => 'x';
const narrow: readonly (() => void)[] = [make];
const wide: readonly (() => number | void)[] = narrow;
for (const wider of wide) {
const next = wider();
if (next) console.log(next.toFixed(1));
}
