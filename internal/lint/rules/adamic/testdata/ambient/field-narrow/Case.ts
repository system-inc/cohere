class B { x: number | string = "s" }
class D extends B { declare x: number }
new D().x.toFixed();
