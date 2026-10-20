// t98g: != against a whole enum type, not a member literal
enum Kind { A, B }
enum Other { X, Y }
type P = { kind: Kind; value: string };
type Q = { kind: Other.X; value: number };
function f(v: P | Q): string {
  if (v.kind != Other.X) {
    return v.value.toUpperCase();
  }
  return v.value.toFixed(1);
}
console.log(f({ kind: Kind.A, value: 'text1' }));
export {};
