const make: () => void = () => 'x';
const narrow: readonly [() => void] = [make];
const wide: readonly [() => number | void] = narrow;
const wider = wide[0];
const next = wider();
if (next) console.log(next.toFixed(1));
