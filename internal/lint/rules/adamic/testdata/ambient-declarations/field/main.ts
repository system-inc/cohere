class Base { readonly x: number = 1; }
class Defined extends Base { declare readonly x: number; }
console.log(String(new Defined().x + 1));
export {};
