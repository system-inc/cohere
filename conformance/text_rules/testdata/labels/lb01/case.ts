export function findTotal(grid: number[][]): number {
  let found = 0
  outer: for (const row of grid) {
    for (const cell of row) {
      if (cell < 0) {
        continue outer
      }
      if (cell > 9) {
        break outer
      }
      found += cell
    }
  }
  return found
}
