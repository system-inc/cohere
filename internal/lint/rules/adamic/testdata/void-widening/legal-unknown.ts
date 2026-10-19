const make: () => void = () => 42;
const wide: () => unknown = make;
const next = wide();
if (typeof next === "number") console.log(next.toFixed(1));
