func sumAllProbe(_ values: [Int]) -> Int {
    var total = 0
    outer: for value in values {
        total += value
    }
    return total
}
