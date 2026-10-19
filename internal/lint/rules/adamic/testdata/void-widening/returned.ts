const make: () => void = () => 'x';
const wrap = () => make;
const wide: () => () => number | void = wrap;
const wider = wide();
const next = wider();
if (next) console.log(next.toFixed(1));
