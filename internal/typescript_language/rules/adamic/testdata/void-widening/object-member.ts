const make: () => void = () => 'x';
const wider: () => { value: number } | void = make;
const next = wider();
if (next) { next.value.toFixed(1); }
