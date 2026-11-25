// t98e: a switch case
enum Kind { A, B }
enum Other { X, Y }
type P = { kind: Kind.A; value: string };
type Q = { kind: Other.X; value: number };
function f(v: P | Q): string {
  switch (v.kind) {
    case Other.X:
      return v.value.toFixed(1);
    default:
      return v.value.toUpperCase();
  }
}
console.log(f({ kind: Kind.A, value: 'text1' }));
export {};
