import Foundation

/*
 The word list, cohere's `policy/Words.txt`, the same file the Go engine embeds, so Swift and Go read one list of
 the words a run-together folder name splits into (`consistency-require-separated-folder-words`): cspell's en_US and
 software-terms dictionaries, lowercased, with the house's own words merged in.

 The words are compiled in (`PolicyWords`, generated from the file), so a released engine carries them with no
 checkout and nothing handed to it at run time. A test fails while the compiled-in copy and the file differ. A copy
 that would not load refuses the run with exit 2 (`Pipeline`), never a rule that quietly splits nothing.

 It is 155,000 lines that one rule reads, so it is read lazily, as Go's `KnownWords` is: the first `compiledIn()`
 reads it, every later one is handed the same list, and a run that never reaches the rule (`--rules`, or the rule
 turned off) never reads it at all.
 */
public struct WordList: Sendable {
    struct LoadFailure: Error, CustomStringConvertible {
        var description: String
    }

    /* Every word once, lowercase ASCII letters alone. */
    private(set) var words: Set<String> = []

    /* An empty list holds no word, which is right only where nothing is judged: listing rule names, or a run with the rule off. */
    public init() {}

    /* How many words the list holds. */
    var count: Int { words.count }

    /* Whether the list holds the word, spelled exactly: the list is lowercase, so a capital never matches. */
    func contains(_ word: String) -> Bool {
        words.contains(word)
    }

    /* The compiled-in file, read the first time a caller asks and never again: a static `let` is initialized once, on first use. */
    private static let compiled = Result { () throws(LoadFailure) in try load(text: PolicyWords.file) }

    /* The compiled-in words. The reason alone, in words: the caller says what failed to load, once, in the sentence a person reads. */
    public static func compiledIn() throws -> WordList {
        try compiled.get()
    }

    /*
     Go's `loadWords`, check for check, so a file one engine refuses the other refuses too: the comment lines that
     open it, then one word a line, each lowercase ASCII letters alone and after the one before it in byte order, so a
     duplicate, a misplaced word or a hand edit out of order is refused rather than read. A comment after the first
     word is refused, and so is a file that does not end with a newline, whose last word may be cut short. Read as
     bytes, never as characters, since Swift reads `\r\n` as one character and Go reads a carriage return as a byte
     no word holds.
     */
    static func load(text: String) throws(LoadFailure) -> WordList {
        let bytes = Array(text.utf8)
        guard bytes.last == UInt8(ascii: "\n") else {
            throw LoadFailure(description: "the file does not end with a newline, so its last word may be cut short")
        }
        var list = WordList()
        var previous: ArraySlice<UInt8>?
        var lineStart = 0
        var lineNumber = 0
        while lineStart < bytes.count {
            lineNumber += 1
            let lineEnd = bytes[lineStart...].firstIndex(of: UInt8(ascii: "\n")) ?? bytes.count
            let line = bytes[lineStart..<lineEnd]
            lineStart = lineEnd + 1
            if previous == nil, line.first == UInt8(ascii: "#") {
                continue
            }
            let lineText = String(decoding: line, as: UTF8.self)
            guard !line.isEmpty, line.allSatisfy({ $0 >= UInt8(ascii: "a") && $0 <= UInt8(ascii: "z") }) else {
                throw LoadFailure(
                    description: "line \(lineNumber), \"\(lineText)\", is not lowercase ASCII letters alone"
                )
            }
            if let previous, !previous.lexicographicallyPrecedes(line) {
                throw LoadFailure(
                    description:
                        "line \(lineNumber), \"\(lineText)\", does not come after \"\(String(decoding: previous, as: UTF8.self))\", so the list is not sorted and deduplicated"
                )
            }
            list.words.insert(lineText)
            previous = line
        }
        guard !list.words.isEmpty else {
            throw LoadFailure(description: "the file holds no words")
        }
        return list
    }
}
