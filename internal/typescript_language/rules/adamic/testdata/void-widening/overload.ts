function make(): void;
function make() { return 'x'; }
const wider: () => number | void = make;
const next = wider();
if (next) console.log(next.toFixed(1));
