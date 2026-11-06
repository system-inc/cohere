func safelyProbe() -> String {
    do {
        return try String(contentsOfFile: "x", encoding: .utf8)
    } catch let e {
        return "\(e)"
    }
}
