function makeCallback(): () => void { return () => 'x'; }
const wider: () => number | void = makeCallback();
const next = wider();
if (next) console.log(next.toFixed(1));
