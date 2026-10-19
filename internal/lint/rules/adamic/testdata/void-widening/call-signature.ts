interface Callback { (): void }
const make: Callback = () => 'x';
const wider: () => number | void = make;
const next = wider();
if (next) console.log(next.toFixed(1));
