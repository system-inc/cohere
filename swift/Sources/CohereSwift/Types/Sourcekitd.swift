import Foundation
import SourcekitdShim
import Synchronization

/*
 The Swift compiler as a library in this process: `sourcekitdInProc`, the parser, Clang importer and type
 checker Xcode and SourceKit-LSP run, loaded with dlopen from the toolchain `xcrun` names.

 This is the door the design calls (b). It answers per file, in the context of the file's module, with the
 compiler's own diagnostics and the type of every expression, so a rule can ask what a value is instead of
 guessing from its spelling. Measured on ahraos-macos: the first file of a process 0.19 to 1.1s, later
 files 0.09 to 0.25s each; an injected `let x: Int = "nope"` comes back as the compiler's own error at its
 line and column, and a `var` never mutated as its warning with the fix-it.

 One process holds one session, `shared`, initialized once and never shut down: the compiler inside
 registers its classes at `sourcekitd_initialize`, and a second initialization in the same process aborts
 it ("Double registration of class MultipleValueInstructionResult", measured when a second test made a
 second session). The session builds one AST at a time, and requests sent from several threads at once
 cancel each other (21 of 22 in the design's probe, even with `key.cancel_on_subsequent_request: 0`), so
 each question holds a lock from opening its document to closing it.

 A file is opened as an editor document before it is asked about, because a cold `source.request.diagnostics`
 on a file nobody opened returned an empty answer in the design's first probe. It is closed afterwards, so
 the session does not hold every AST of a 472-file module at once.
 */
final class Sourcekitd: Sendable {
    /* The library could not be found or loaded, or answered a request with an error. */
    struct Failure: Error, CustomStringConvertible {
        var description: String
    }

    /* One compiler diagnostic, placed the way `.dia` records place theirs: 1-based line, 1-based UTF-8 byte column. */
    struct Diagnostic: Decodable, Equatable, Sendable {
        var line: Int
        var column: Int
        var file: String
        var severity: String
        var identifier: String
        var message: String
        /* The diagnostic's documentation pages; the last path segment of one is its warning group, in kebab case. */
        var educationalNotes: [String]?

        enum CodingKeys: String, CodingKey {
            case line = "key.line"
            case column = "key.column"
            case file = "key.filepath"
            case severity = "key.severity"
            case identifier = "key.id"
            case message = "key.description"
            case educationalNotes = "key.educational_note_paths"
        }

        var findingSeverity: FindingRecord.Severity? {
            switch severity {
            case "source.diagnostic.severity.error": .error
            case "source.diagnostic.severity.warning": .warning
            default: nil
            }
        }
    }

    /* The type of one expression: its 0-based UTF-8 byte range and the type as the compiler prints it. */
    struct ExpressionType: Decodable, Equatable, Sendable {
        var offset: Int
        var length: Int
        var type: String

        enum CodingKeys: String, CodingKey {
            case offset = "key.expression_offset"
            case length = "key.expression_length"
            case type = "key.expression_type"
        }
    }

    private struct DiagnosticsResponse: Decodable {
        var diagnostics: [Diagnostic]?

        enum CodingKeys: String, CodingKey {
            case diagnostics = "key.diagnostics"
        }
    }

    /* One name `indexsource` reports, and the names inside it: a declaration's body holds its references. */
    private struct IndexedEntity: Decodable, Sendable {
        var kind: String
        var name: String?
        var symbol: String?
        var line: Int?
        var column: Int?
        var entities: [IndexedEntity]?

        enum CodingKeys: String, CodingKey {
            case kind = "key.kind"
            case name = "key.name"
            case symbol = "key.usr"
            case line = "key.line"
            case column = "key.column"
            case entities = "key.entities"
        }
    }

    private struct IndexResponse: Decodable, Sendable {
        var entities: [IndexedEntity]?

        enum CodingKeys: String, CodingKey {
            case entities = "key.entities"
        }
    }

    /* An answer read only for whether it was an error: opening and closing a document. */
    private struct Acknowledgement: Decodable {}

    private struct ExpressionTypesResponse: Decodable {
        var types: [ExpressionType]?

        enum CodingKeys: String, CodingKey {
            case types = "key.expression_type_list"
        }
    }

    private let uidFromString: CohereSourcekitdUidGetFromCString
    private let dictionaryCreate: CohereSourcekitdRequestDictionaryCreate
    private let dictionarySetString: CohereSourcekitdRequestDictionarySetString
    private let dictionarySetUid: CohereSourcekitdRequestDictionarySetUid
    private let dictionarySetInteger: CohereSourcekitdRequestDictionarySetInt64
    private let dictionarySetValue: CohereSourcekitdRequestDictionarySetValue
    private let arrayCreate: CohereSourcekitdRequestArrayCreate
    private let arraySetString: CohereSourcekitdRequestArraySetString
    private let requestRelease: CohereSourcekitdRequestRelease
    private let sendSynchronously: CohereSourcekitdSendRequestSync
    private let responseIsError: CohereSourcekitdResponseIsError
    private let errorDescription: CohereSourcekitdResponseErrorGetDescription
    private let responseValue: CohereSourcekitdResponseGetValue
    private let jsonDescription: CohereSourcekitdVariantJsonDescriptionCopy
    private let responseDispose: CohereSourcekitdResponseDispose
    /* Held for one whole question, open to close, so two callers never interleave requests. */
    private let asking = Mutex(())

    /* The process's session, loaded the first time anyone asks. A load that failed is retried, since nothing was initialized. */
    private static let loaded = Mutex<Sourcekitd?>(nil)

    static func shared(runner: ProcessRunner = ProcessRunner()) throws -> Sourcekitd {
        try loaded.withLock { session in
            if let session {
                return session
            }
            let created = try Sourcekitd(libraryPath: toolchainLibraryPath(runner: runner))
            session = created
            return created
        }
    }

    private init(libraryPath: String) throws {
        guard let handle = dlopen(libraryPath, RTLD_NOW | RTLD_LOCAL) else {
            let reason = dlerror().map { String(cString: $0) } ?? "no reason given"
            throw Failure(description: "could not load sourcekitd at \(libraryPath): \(reason)")
        }
        func symbol<Function>(_ name: String, as type: Function.Type) throws -> Function {
            guard let address = dlsym(handle, name) else {
                throw Failure(description: "sourcekitd at \(libraryPath) has no \(name)")
            }
            return unsafeBitCast(address, to: type)
        }
        uidFromString = try symbol("sourcekitd_uid_get_from_cstr", as: CohereSourcekitdUidGetFromCString.self)
        dictionaryCreate = try symbol("sourcekitd_request_dictionary_create", as: CohereSourcekitdRequestDictionaryCreate.self)
        dictionarySetString = try symbol("sourcekitd_request_dictionary_set_string", as: CohereSourcekitdRequestDictionarySetString.self)
        dictionarySetUid = try symbol("sourcekitd_request_dictionary_set_uid", as: CohereSourcekitdRequestDictionarySetUid.self)
        dictionarySetInteger = try symbol("sourcekitd_request_dictionary_set_int64", as: CohereSourcekitdRequestDictionarySetInt64.self)
        dictionarySetValue = try symbol("sourcekitd_request_dictionary_set_value", as: CohereSourcekitdRequestDictionarySetValue.self)
        arrayCreate = try symbol("sourcekitd_request_array_create", as: CohereSourcekitdRequestArrayCreate.self)
        arraySetString = try symbol("sourcekitd_request_array_set_string", as: CohereSourcekitdRequestArraySetString.self)
        requestRelease = try symbol("sourcekitd_request_release", as: CohereSourcekitdRequestRelease.self)
        sendSynchronously = try symbol("sourcekitd_send_request_sync", as: CohereSourcekitdSendRequestSync.self)
        responseIsError = try symbol("sourcekitd_response_is_error", as: CohereSourcekitdResponseIsError.self)
        errorDescription = try symbol("sourcekitd_response_error_get_description", as: CohereSourcekitdResponseErrorGetDescription.self)
        responseValue = try symbol("sourcekitd_response_get_value", as: CohereSourcekitdResponseGetValue.self)
        jsonDescription = try symbol("sourcekitd_variant_json_description_copy", as: CohereSourcekitdVariantJsonDescriptionCopy.self)
        responseDispose = try symbol("sourcekitd_response_dispose", as: CohereSourcekitdResponseDispose.self)
        let initialize = try symbol("sourcekitd_initialize", as: CohereSourcekitdInitialize.self)
        initialize()
    }

    /* The sourcekitd beside the `swift` that `xcrun` resolves: the toolchain that also builds the checked package. */
    static func toolchainLibraryPath(runner: ProcessRunner = ProcessRunner()) throws -> String {
        let library = try SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner)
        return URL(fileURLWithPath: library)
            .deletingLastPathComponent()
            .appendingPathComponent("sourcekitdInProc.framework/sourcekitdInProc")
            .path
    }

    /*
     The compiler's diagnostics for one file, in the context of its module's other files. `text`, when given,
     is checked in place of what is on disk, which is how the tests inject an error without writing a file.
     */
    func diagnostics(file: String, arguments: [String], text: String? = nil) throws -> [Diagnostic] {
        try withOpenDocument(file: file, arguments: arguments, text: text) {
            let response: DiagnosticsResponse = try send("source.request.diagnostics") { request in
                set(request, "key.sourcefile", file)
                set(request, "key.compilerargs", arguments)
            }
            return response.diagnostics ?? []
        }
    }

    /* The type of every expression in one file, after type checking. */
    func expressionTypes(file: String, arguments: [String], text: String? = nil) throws -> [ExpressionType] {
        try withOpenDocument(file: file, arguments: arguments, text: text) {
            let response: ExpressionTypesResponse = try send("source.request.expression.type") { request in
                set(request, "key.sourcefile", file)
                set(request, "key.compilerargs", arguments)
            }
            return response.types ?? []
        }
    }

    /*
     Every name in one file and the declaration it resolves to: what the build's index store records, asked of
     the compiler here, for a file the build did not compile as it stands. A kind spelled `source.lang.swift.ref.`
     is a reference; every other kind declares. An accessor reference is the implicit getter or setter call the
     index store marks implicit, so it is marked the same way here.
     */
    func symbols(file: String, arguments: [String]) throws -> FileSymbols {
        let response: IndexResponse = try asking.withLock { _ in
            try send("source.request.indexsource") { request in
                set(request, "key.sourcefile", file)
                set(request, "key.compilerargs", arguments)
            }
        }
        var occurrences: [FileSymbols.Occurrence] = []
        var pending = response.entities ?? []
        while let entity = pending.popLast() {
            pending.append(contentsOf: entity.entities ?? [])
            guard let symbol = entity.symbol, let line = entity.line, let column = entity.column else { continue }
            occurrences.append(FileSymbols.Occurrence(
                line: line,
                column: column,
                symbol: symbol,
                name: entity.name ?? "",
                isReference: entity.kind.hasPrefix("source.lang.swift.ref."),
                isImplicit: entity.kind.contains(".accessor.")
            ))
        }
        return FileSymbols(occurrences)
    }

    private func withOpenDocument<Answer: Sendable>(file: String, arguments: [String], text: String?, _ body: () throws -> Answer) throws -> Answer {
        try asking.withLock { _ in try answerWithOpenDocument(file: file, arguments: arguments, text: text, body) }
    }

    private func answerWithOpenDocument<Answer>(file: String, arguments: [String], text: String?, _ body: () throws -> Answer) throws -> Answer {
        let _: Acknowledgement = try send("source.request.editor.open") { request in
            set(request, "key.name", file)
            set(request, "key.sourcefile", file)
            if let text {
                set(request, "key.sourcetext", text)
            }
            set(request, "key.compilerargs", arguments)
            set(request, "key.enablesyntaxmap", 0)
            set(request, "key.enablesubstructure", 0)
            set(request, "key.syntactic_only", 0)
        }
        defer {
            let _: Acknowledgement? = try? send("source.request.editor.close") { request in
                set(request, "key.name", file)
            }
        }
        return try body()
    }

    /* One request, answered synchronously, its response decoded from the JSON sourcekitd describes it as. */
    private func send<Response: Decodable>(_ kind: String, _ fill: (CohereSourcekitdObject) -> Void) throws -> Response {
        guard let request = dictionaryCreate(nil, nil, 0) else {
            throw Failure(description: "sourcekitd could not create a request")
        }
        defer { requestRelease(request) }
        dictionarySetUid(request, uid("key.request"), uid(kind))
        fill(request)
        guard let response = sendSynchronously(request) else {
            throw Failure(description: "sourcekitd returned no response to \(kind)")
        }
        defer { responseDispose(response) }
        if responseIsError(response) {
            let reason = errorDescription(response).map { String(cString: $0) } ?? "no reason given"
            throw Failure(description: "sourcekitd answered \(kind) with an error: \(reason)")
        }
        guard let json = jsonDescription(responseValue(response)) else {
            throw Failure(description: "sourcekitd's answer to \(kind) could not be described")
        }
        defer { free(json) }
        return try JSONDecoder().decode(Response.self, from: Data(String(cString: json).utf8))
    }

    /* `SOURCEKITD_ARRAY_APPEND`, `(size_t)-1`: `size_t` arrives in Swift as `Int`, so its all-ones bit pattern is -1. */
    private static let arrayAppend = Int(bitPattern: UInt.max)

    private func uid(_ name: String) -> CohereSourcekitdUid? {
        uidFromString(name)
    }

    private func set(_ request: CohereSourcekitdObject, _ key: String, _ value: String) {
        dictionarySetString(request, uid(key), value)
    }

    private func set(_ request: CohereSourcekitdObject, _ key: String, _ value: Int64) {
        dictionarySetInteger(request, uid(key), value)
    }

    private func set(_ request: CohereSourcekitdObject, _ key: String, _ values: [String]) {
        guard let array = arrayCreate(nil, 0) else { return }
        for value in values {
            arraySetString(array, Self.arrayAppend, value)
        }
        dictionarySetValue(request, uid(key), array)
        requestRelease(array)
    }
}
