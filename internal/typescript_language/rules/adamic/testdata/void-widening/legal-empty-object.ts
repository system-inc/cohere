const make: () => void = () => 'x';
const wide: () => {} | void = make;
const next = wide();
if (next) console.log(next.toString());
