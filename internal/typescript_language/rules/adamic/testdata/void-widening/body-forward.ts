const hidden: () => void = () => 'x';
function make(): void { return hidden(); }
const wider: () => number | void = make;
const next = wider();
if (next) console.log(next.toFixed(1));
