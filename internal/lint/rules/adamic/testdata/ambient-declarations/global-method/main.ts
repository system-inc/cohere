export {};
declare global { interface Array<T> { missing(): T; } }
console.log(String([1].missing()));
