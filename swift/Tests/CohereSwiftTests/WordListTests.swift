import Foundation
import Testing

@testable import CohereSwift

/*
 The word list's loader, held to the Go loader's own vectors (`policy/words_test.go`), so a file one engine refuses
 the other refuses too, and the compiled-in copy, held byte for byte to `policy/Words.txt`.
 */
struct WordListTests {
    /* A file every refusal below starts from, changed in one place, so each refusal is the one under test rather than some other. */
    static let minimal = "# a header line\n# and a second\nalpha\nbeta\ngamma\n"

    @Test(arguments: [
        ("alpha", true),
        ("beta", true),
        ("gamma", true),
        ("Alpha", false),
        ("alph", false),
        ("delta", false),
        ("# a header line", false),
        ("", false),
    ])
    func theMinimalFileIsReadWithoutItsHeader(word: String, held: Bool) throws {
        let list = try WordList.load(text: Self.minimal)
        #expect(list.count == 3)
        #expect(list.contains(word) == held)
    }

    @Test(arguments: [
        ("a capital", "beta", "Beta"),
        ("a digit", "beta", "beta2"),
        ("an apostrophe", "beta", "be'ta"),
        ("a space", "beta", "be ta"),
        ("a letter outside ASCII", "beta", "b\u{E9}ta"),
        ("an empty line", "beta\n", "beta\n\n"),
        ("a carriage return", "beta\n", "beta\r\n"),
        ("a duplicate", "beta", "alpha"),
        ("a word out of order", "beta\ngamma", "gamma\nbeta"),
        ("a comment after the first word", "beta", "# beta"),
        ("no final newline", "gamma\n", "gamma"),
        ("no words", "alpha\nbeta\ngamma\n", ""),
    ])
    func eachOnePlaceChangeIsRefused(name: String, original: String, replacement: String) throws {
        let range = try #require(Self.minimal.range(of: original), "\(name): the original is not in the file")
        let changed = Self.minimal.replacingCharacters(in: range, with: replacement)
        #expect(changed != Self.minimal, "\(name): the replacement changed nothing, so the case tests nothing")
        #expect(throws: WordList.LoadFailure.self, "\(name) is read, and the loader refuses it") {
            try WordList.load(text: changed)
        }
    }

    /* The compiled-in list loads, holds both lists whole, and reads the words the folder rule leans on. */
    @Test func theCompiledInListLoads() throws {
        let list = try WordList.compiledIn()
        #expect(list.count >= 150_000, "Words.txt holds \(list.count) words, fewer than en_US alone")
        for word in ["format", "options", "allowed", "helpers", "workspace", "javascript", "cgo"] {
            #expect(list.contains(word), "Words.txt lacks \(word)")
        }
        #expect(!list.contains("formatoptions"), "Words.txt holds formatoptions, two words run together")
    }

    /*
     The compiled-in words are cohere's `policy/Words.txt`, byte for byte, for the folder rule to read as the Go rule
     does. A word added to the file and not regenerated fails here.
     */
    @Test func theCompiledInWordsArePolicysFile() throws {
        let policyFile = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("policy/Words.txt")
        let onDisk = try Data(contentsOf: policyFile)
        #expect(!onDisk.isEmpty)
        #expect(Data(PolicyWords.file.utf8) == onDisk, "run go run ./policy/tools/generate")
    }
}
