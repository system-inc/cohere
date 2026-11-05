import Foundation

/*
 The few lines of an xcodegen spec a toolchain finding points at and its suggestion edits: each target's line under
 the top-level `targets:`, and the target's `settings:` map, where a build setting is added.

 The edit is offered as a suggestion and never applied. Turning Swift 6, an upcoming feature or strict memory safety
 on is migration work: on CapturePhone it left 20 type errors, and strict memory safety on presence's packages was
 2,103 sites. A fix that writes a build that no longer compiles is worse than none, which is also why the same rules
 offer no fix in a Package.swift.

 Read from the text, line by line, rather than through a YAML parser, because the edit must leave every other byte
 as it was, comments and order included, and a parser's printer would rewrite the file. So this reads only the
 plain block shape xcodegen's own examples are written in, and refuses the rest: a target or a settings map written
 as a flow map (`{ ... }`), an anchor or an alias (`&`, `*`), a merge key (`<<`), a `groups:` preset, a tab in the
 indentation, a carriage return. A refusal is never a guess; the finding stands with no suggestion.

 How xcodegen reads a target's settings decides where a key goes (ProjectSpec.md, `Settings`): a map holding
 `base`, `configs` or `groups` is the structured form, whose `base` applies to every configuration, and any other
 map is itself the base. So a key is added under `base:` in the structured form, creating it when only `configs`
 is there, and straight into a plain map; a target with no `settings:` gets `settings:` and `base:`. A key already
 there has its value replaced, since YAML refuses a key written twice. A key one of the target's own `configs:`
 sets to another value is resolved after `base`, so writing it in `base` would change nothing there, and no edit is
 offered; one that sets the same value already agrees.
 */
struct XcodeGenSpecification {
    /* One build setting as the spec spells it: `SWIFT_VERSION` and `"6.0"`, or a feature's key and `YES`. */
    struct Setting: Equatable, Hashable {
        var key: String
        var value: String
    }

    /* Why no edit is offered for a target's settings. */
    enum Refusal: Error, Equatable {
        /* Not under this file's `targets:`, as a target from an included file is not. */
        case targetNotFound
        /* A preset group, an anchor, an alias, a merge key or a flow map. */
        case notPlain
        /* A tab in the indentation or a carriage return. */
        case unreadable
        /* One of the target's own configurations sets the key, which a key in `base` would not override. */
        case overriddenByConfiguration
        /* The base settings already hold every value, so something resolved after them turns it off. */
        case alreadyWritten
    }

    /* One line of the spec, as read, with its indentation and, when it is a `key: value` line, its key, value and trailing comment. */
    struct Line {
        var raw: String
        var indent: Int
        var content: String
        var key: String?
        var value: String
        var comment: String

        /* Blank and comment lines hold no structure; every other line does. */
        var isSignificant: Bool {
            !content.isEmpty && !content.hasPrefix("#")
        }
    }

    let lines: [Line]
    /* The text holds something this reader does not rewrite (`Refusal.unreadable`), so it offers no edit. */
    let unreadable: Bool

    init(text: String) {
        lines = text.split(separator: "\n", omittingEmptySubsequences: false).map { raw in
            let indentation = raw.prefix { $0 == " " || $0 == "\t" }
            let content = String(raw.dropFirst(indentation.count)).trimmingCharacters(in: .whitespaces)
            let parsed = Self.keyAndValue(of: content)
            return Line(
                raw: String(raw),
                indent: indentation.count,
                content: content,
                key: parsed?.key,
                value: parsed?.value ?? "",
                comment: parsed?.comment ?? "",
            )
        }
        /* By the bytes: a String holds `\r\n` as one character, which neither a search for `\r` nor a split on `\n` sees. */
        unreadable =
            text.utf8.contains(UInt8(ascii: "\r"))
            || lines.contains { $0.raw.prefix { $0 == " " || $0 == "\t" }.contains("\t") }
    }

    /*
     The key, value and trailing comment of a `key: value` line, or nil for anything else: a sequence item, a comment,
     a line with no key. A quoted key is read to its closing quote.
     */
    static func keyAndValue(of content: String) -> (key: String, value: String, comment: String)? {
        guard !content.isEmpty, !content.hasPrefix("#"), !content.hasPrefix("- "), content != "-" else { return nil }
        let key: String
        let rest: Substring
        if let quote = content.first, quote == "\"" || quote == "'" {
            let afterQuote = content.dropFirst()
            guard let closing = afterQuote.firstIndex(of: quote) else { return nil }
            key = String(afterQuote[..<closing])
            rest = afterQuote[afterQuote.index(after: closing)...]
            guard rest.hasPrefix(":") else { return nil }
        }
        else {
            guard
                let colon = content.range(of: ": ")?.lowerBound
                    ?? (content.hasSuffix(":") ? content.index(before: content.endIndex) : nil)
            else { return nil }
            key = String(content[..<colon])
            rest = content[colon...]
        }
        let value = rest.dropFirst()
        let uncommented = withoutComment(value)
        return (
            key,
            String(uncommented).trimmingCharacters(in: .whitespaces),
            String(value[uncommented.endIndex...]).trimmingCharacters(in: .whitespaces),
        )
    }

    /* A value up to a ` #` that starts a comment, outside any quotes. */
    static func withoutComment(_ value: Substring) -> Substring {
        var quote: Character?
        var previous: Character = " "
        for index in value.indices {
            let character = value[index]
            if let open = quote {
                if character == open {
                    quote = nil
                }
            }
            else if character == "\"" || character == "'" {
                quote = character
            }
            else if character == "#" && previous.isWhitespace {
                return value[..<index]
            }
            previous = character
        }
        return value
    }

    /* The index of the top-level `targets:` line, when it opens a block. */
    var targetsLine: Int? {
        lines.indices.first { lines[$0].indent == 0 && lines[$0].key == "targets" && lines[$0].value.isEmpty }
    }

    /*
     The direct children of the block opened at `index`: the significant lines after it, indented deeper, up to the
     first that is not, all at the first one's indentation. Nil when a line inside sits between the two depths, which
     is not a block this reader follows.
     */
    func children(of index: Int) -> (indent: Int, lines: [Int], end: Int)? {
        let parent = lines[index].indent
        var childIndent: Int?
        var children: [Int] = []
        var end = index + 1
        for position in (index + 1)..<lines.count where lines[position].isSignificant {
            if lines[position].indent <= parent {
                break
            }
            end = position + 1
            let indent = childIndent ?? lines[position].indent
            childIndent = indent
            if lines[position].indent == indent {
                children.append(position)
            }
            else if lines[position].indent < indent {
                return nil
            }
        }
        return (childIndent ?? parent, children, end)
    }

    /* Each target's line, 1-based, by name. */
    var targetLines: [String: Int] {
        guard let targetsLine, let targets = children(of: targetsLine) else { return [:] }
        var found: [String: Int] = [:]
        for index in targets.lines {
            if let name = lines[index].key {
                found[name] = index + 1
            }
        }
        return found
    }

    /*
     The spec's text with `settings` written into `target`'s base settings, or the refusal that says why not. The
     indentation step is the one the target's own block uses.
     */
    func adding(_ settings: [Setting], to target: String) -> Result<String, Refusal> {
        guard !unreadable else { return .failure(.unreadable) }
        guard let targetsLine, let targets = children(of: targetsLine),
            let targetIndex = targets.lines.first(where: { lines[$0].key == target })
        else { return .failure(.targetNotFound) }
        guard lines[targetIndex].value.isEmpty, let body = children(of: targetIndex),
            body.indent > lines[targetIndex].indent
        else { return .failure(.notPlain) }
        let step = body.indent - lines[targetIndex].indent
        let pad = { (indent: Int) in String(repeating: " ", count: indent) }
        let written = { (indent: Int, setting: Setting) in "\(pad(indent))\(setting.key): \(setting.value)" }
        var edited = lines.map(\.raw)

        guard let settingsIndex = body.lines.first(where: { lines[$0].key == "settings" }) else {
            let added =
                ["\(pad(body.indent))settings:", "\(pad(body.indent + step))base:"]
                + settings.map { written(body.indent + 2 * step, $0) }
            edited.insert(contentsOf: added, at: targetIndex + 1)
            return .success(rejoined(edited))
        }
        guard lines[settingsIndex].value.isEmpty, let settingsBody = children(of: settingsIndex) else {
            return .failure(.notPlain)
        }
        let settingsKeys = settingsBody.lines.map { lines[$0].key }
        guard !settingsKeys.contains(nil), !settingsKeys.contains("groups"), !settingsKeys.contains("<<") else {
            return .failure(.notPlain)
        }
        let structured = settingsKeys.contains("base") || settingsKeys.contains("configs")
        if let configsIndex = settingsBody.lines.first(where: { lines[$0].key == "configs" }) {
            guard lines[configsIndex].value.isEmpty, let configs = children(of: configsIndex) else {
                return .failure(.notPlain)
            }
            for configuration in configs.lines {
                guard lines[configuration].value.isEmpty, let configured = children(of: configuration) else {
                    return .failure(.notPlain)
                }
                /* A configuration that sets the same value already agrees; only another value would win over base. */
                let unquoted = { (value: String) in value.trimmingCharacters(in: CharacterSet(charactersIn: "\"'")) }
                let overriding = settings.contains { setting in
                    configured.lines.contains { line in
                        lines[line].key == setting.key && unquoted(lines[line].value) != unquoted(setting.value)
                    }
                }
                if overriding {
                    return .failure(.overriddenByConfiguration)
                }
            }
        }
        let mapIndex: Int
        let map: (indent: Int, lines: [Int], end: Int)
        if !structured {
            mapIndex = settingsIndex
            map =
                settingsBody.lines.isEmpty ? (lines[settingsIndex].indent + step, [], settingsIndex + 1) : settingsBody
        }
        else if let baseIndex = settingsBody.lines.first(where: { lines[$0].key == "base" }) {
            guard lines[baseIndex].value.isEmpty, let baseBody = children(of: baseIndex) else {
                return .failure(.notPlain)
            }
            mapIndex = baseIndex
            map = baseBody.lines.isEmpty ? (lines[baseIndex].indent + step, [], baseIndex + 1) : baseBody
        }
        else {
            let added =
                ["\(pad(settingsBody.indent))base:"] + settings.map { written(settingsBody.indent + step, $0) }
            edited.insert(contentsOf: added, at: settingsIndex + 1)
            return .success(rejoined(edited))
        }
        guard map.lines.allSatisfy({ lines[$0].key != nil && lines[$0].key != "<<" }) else {
            return .failure(.notPlain)
        }
        var inserted: [String] = []
        for setting in settings {
            if let existing = map.lines.first(where: { lines[$0].key == setting.key }) {
                guard !lines[existing].value.isEmpty else { return .failure(.notPlain) }
                let comment = lines[existing].comment
                edited[existing] = written(map.indent, setting) + (comment.isEmpty ? "" : " \(comment)")
            }
            else {
                inserted.append(written(map.indent, setting))
            }
        }
        edited.insert(contentsOf: inserted, at: mapIndex + 1)
        return .success(rejoined(edited))
    }

    /*
     The edit a suggestion carries for `settings` in `target`: the smallest replacement, as UTF-8 byte offsets into
     the spec as read, that turns it into the text `adding` writes, so applying it touches only the lines it adds or
     changes.
     */
    func suggestedEdit(_ settings: [Setting], to target: String) -> Result<FindingRecord.Edit, Refusal> {
        let edited: String
        switch adding(settings, to: target) {
            case let .success(written):
                edited = written
            case let .failure(refusal):
                return .failure(refusal)
        }
        let before = Array(rejoined(lines.map(\.raw)).utf8)
        let after = Array(edited.utf8)
        guard before != after else { return .failure(.alreadyWritten) }
        var prefix = 0
        while prefix < before.count && prefix < after.count && before[prefix] == after[prefix] {
            prefix += 1
        }
        var suffix = 0
        while suffix < before.count - prefix && suffix < after.count - prefix
            && before[before.count - 1 - suffix] == after[after.count - 1 - suffix]
        {
            suffix += 1
        }
        /* Back to whole characters, so neither end of the edit splits a multibyte one (a continuation byte is 10xxxxxx). */
        let isContinuation = { (byte: UInt8) in byte & 0b1100_0000 == 0b1000_0000 }
        while prefix > 0 && prefix < before.count && isContinuation(before[prefix]) {
            prefix -= 1
        }
        while suffix > 0 && isContinuation(before[before.count - suffix]) {
            suffix -= 1
        }
        return .success(
            FindingRecord.Edit(
                start: prefix,
                end: before.count - suffix,
                text: String(decoding: after[prefix..<(after.count - suffix)], as: UTF8.self),
            )
        )
    }

    /* The lines as text again, each untouched line exactly as it was read. */
    private func rejoined(_ edited: [String]) -> String {
        edited.joined(separator: "\n")
    }
}
