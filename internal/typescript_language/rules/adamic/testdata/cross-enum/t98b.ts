// t98b: one member's tag is a union holding both enums (Adamic's cross_enum_union)
enum Kind { A, B }
enum Other { X, Y }
type P = { kind: Kind.A | Other.X; value: string };
type Q = { kind: Kind.A; value: number };
function f(v: P | Q): string {
  if (v.kind === Other.X) {
    return v.value.toUpperCase();
  }
  return 'q:' + String(v.value);
}
const q: Q = { kind: Kind.A, value: 1 };
console.log(f(q));
export {};
