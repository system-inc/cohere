const make: () => void = () => 'x';
const wider: () => any = make;
console.log(wider().toFixed(1));
