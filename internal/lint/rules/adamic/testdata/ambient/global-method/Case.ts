export {};
declare global { interface Array<T> { missing(): T } }
[1].missing().toFixed();
