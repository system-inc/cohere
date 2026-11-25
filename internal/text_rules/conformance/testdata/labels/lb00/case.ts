export function sumAll(values: number[]): number {
  let total = 0
  outer: for (const value of values) {
    total += value
  }
  return total
}
