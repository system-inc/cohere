import Foundation
import Testing

@testable import CohereSwift

/* `git status --porcelain=v1 -z` entries, including the rename form whose second field is the old path rather than a changed file. */
struct FileScopeTests {
    static func porcelain(_ fields: [String]) -> Data {
        Data(fields.map { $0 + "\0" }.joined().utf8)
    }

    @Test func modifiedAndUntrackedFilesAreChanged() {
        let paths = FileScope.changedPaths(fromPorcelain: Self.porcelain([" M Sources/A.swift", "?? Sources/New.swift"]))
        #expect(paths == ["Sources/A.swift", "Sources/New.swift"])
    }

    @Test func aRenameCountsItsNewPathOnly() {
        let paths = FileScope.changedPaths(fromPorcelain: Self.porcelain(["R  Sources/New.swift", "Sources/Old.swift", " M Sources/B.swift"]))
        #expect(paths == ["Sources/New.swift", "Sources/B.swift"])
    }

    @Test func nothingChangedIsEmpty() {
        #expect(FileScope.changedPaths(fromPorcelain: Data()).isEmpty)
    }
}
