// unknown: string enums and a sibling with an unknown tag
enum Kind { A = 'a', B = 'b' }
enum Other { X = 'a', Y = 'y' }
type P = { kind: Kind.A; value: string };
type Q = { kind: unknown; value: number };
function f(v: P | Q): string {
  if (v.kind === Other.X) {
    return v.value.toFixed(1);
  }
  return 'other';
}
console.log(f({ kind: Kind.A, value: 'text1' }));
export {};
