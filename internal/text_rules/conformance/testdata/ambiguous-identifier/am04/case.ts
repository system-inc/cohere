export function sortedCopy(values: number[]): number[] {
  return values.toSorted((a, b) => a - b)
}
