class B { readonly x: number = 1 }
class D extends B { declare readonly x: number }
new D().x.toFixed();
