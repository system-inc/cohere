const make: () => void = () => 'x';
const narrow: { readonly fn: () => void } = { fn: make };
const wide: { readonly fn: () => number | void } = narrow;
const wider = wide.fn;
const next = wider();
if (next) console.log(next.toFixed(1));
