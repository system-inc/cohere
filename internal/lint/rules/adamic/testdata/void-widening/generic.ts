const make: () => void = () => 'x';
function widen<T>(fn: () => T): () => T { return fn; }
const wider = widen<number | void>(make);
const next = wider();
if (next) console.log(next.toFixed(1));
