const make: () => void = () => 'x';
const wide: () => number | unknown = make;
const next = wide();
if (typeof next === 'number') console.log(next.toFixed(1));
