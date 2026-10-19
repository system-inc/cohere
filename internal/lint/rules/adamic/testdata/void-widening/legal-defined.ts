const make = () => 42;
const wider: () => number | void = make;
const next = wider();
if (next) console.log(next.toFixed(1));
