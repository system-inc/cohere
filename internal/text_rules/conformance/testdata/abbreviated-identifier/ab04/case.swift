func parityProbe() -> Int {
    let userMsg = "a"
    let msgCount = 1
    let ctxValue = 2
    let pageIdx = 3
    let errMsg = "b"
    return userMsg.count + msgCount + ctxValue + pageIdx + errMsg.count
}
