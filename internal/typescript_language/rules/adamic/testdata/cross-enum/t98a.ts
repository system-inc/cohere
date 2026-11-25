// t98a: a discriminant compared to another enum's member that shares a value
enum Kind { A, B }
enum Other { X, Y }
type P = { kind: Kind.A; value: string };
type Q = { kind: Other.X; value: number };
type R = { kind: Kind.B; value: boolean };
function f(v: P | Q | R): string {
  if (v.kind === Other.X) {
    return v.value.toFixed(1);
  }
  return 'other:' + typeof v.value;
}
const p: P = { kind: Kind.A, value: 'text1' };
console.log(f(p));
export {};
