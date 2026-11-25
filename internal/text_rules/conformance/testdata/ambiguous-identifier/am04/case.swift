func largestProbe(_ values: [Int]) -> Int? {
    values.max { a, b in a < b }
}
