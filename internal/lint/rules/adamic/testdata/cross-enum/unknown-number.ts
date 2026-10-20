// unknown: a sibling with an unknown tag
enum Kind { A, B }
enum Other { X, Y }
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
