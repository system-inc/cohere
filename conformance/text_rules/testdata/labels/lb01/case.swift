func findTotalProbe(_ grid: [[Int]]) -> Int {
    var found = 0
    outer: for row in grid {
        for cell in row {
            if cell < 0 {
                continue outer
            }
            if cell > 9 {
                break outer
            }
            found += cell
        }
    }
    return found
}
