import Foundation
import Synchronization

/*
 A client of SwiftPM's Build Server Protocol server, `swift package experimental-build-server`, spoken over the
 server's standard input and output. It is how a package's types are checked without building it (#11fs5xp): the
 server knows every target's exact compiler arguments without compiling anything, and its `buildTarget/prepare` is
 Swift Build's prepare-for-indexing, which emits each dependency's module with bodies skipped and writes no object.

 Measured 2026-11-06 on Swift 6.4.2 against a fixture of a C target, two Swift targets and a test target:
 - `build/initialize` names where the server writes: `indexDatabasePath` is `<scratch>/out`, which holds the prepared
   modules under `Products/<configuration>` and the intermediates beside them, and `indexStorePath` is
   `<scratch>/index-store`, which prepare leaves empty, since prepare writes no index. Both follow `--scratch-path`,
   so a run that passes its own scratch writes nothing into the package's `.build`;
 - `workspace/buildTargets` lists the package manifest too, tagged `not-buildable`, and a test target as its product
   (`FeatureTests-product`), so targets are named by the module their arguments compile;
 - `textDocument/sourceKitOptions` answers one file's arguments, and every Swift file of a target shares them but for
   the trailing `-index-unit-output-path`, so one file per target and language is enough (the probe asked all 415 of
   ahraos-macos's and found the same). A C file's arguments name the file itself, and come with `-fsyntax-only`, an
   `-o` and the build's index flags;
 - the Swift arguments' `-working-directory` is the folder holding the package's root, a C file's the root itself;
   every path in them is absolute, so neither changes what is checked;
 - a bad request is answered with a JSON-RPC error (a bogus target's prepare: -32001, `missingTarget`), and the
   server sends notifications (`build/logMessage`, `buildTarget/didChange`) but never a request of its own.

 One client is one server process, and its requests are asked one at a time from the thread that owns it. A thread of
 its own reads the server's output, so a request can time out while the server is silent. Every error the server
 answers, every timeout and every early exit is a `Failure` that names the step it happened in. Nothing falls back to
 a build: a server that fails is a named gap for the caller to report.
 */
final class BuildServerClient {
    /* What the server is started on, and for which platform. */
    struct Configuration {
        var packageRoot: URL
        /* The scratch the server builds into, never the package's `.build`. */
        var scratchPath: URL
        /* `--triple` and `--sdk`, for a package whose platforms leave out macOS; nil builds for the host. */
        var triple: String?
        var sdk: String?
        /* False holds the server to the package's own Package.resolved (`--disable-automatic-resolution`), as `--no-fix` does. */
        var resolutionAllowed: Bool
        var timeouts = Timeouts()
        /* The environment the server's is made from (`environment(inheriting:workingDirectory:)`): this process's, unless a test hands another. */
        var inherited = ProcessInfo.processInfo.environment
    }

    /*
     How long each request may go unanswered. Prepare compiles every dependency's interface, which took 70s cold on
     ahraos-presence (#nkk91tv's probe), so it has its own, longer bound.
     */
    struct Timeouts {
        var request: TimeInterval = 300
        var prepare: TimeInterval = 1800
        var exit: TimeInterval = 30
    }

    /* Each step of a session, named by the request it sends, so a failure says which one went wrong. */
    enum Step: String, Sendable {
        case launch = "swift package experimental-build-server"
        case initialize = "build/initialize"
        case waitForUpdates = "workspace/waitForBuildSystemUpdates"
        case buildTargets = "workspace/buildTargets"
        case sources = "buildTarget/sources"
        case sourceKitOptions = "textDocument/sourceKitOptions"
        case prepare = "buildTarget/prepare"
        case shutdown = "build/shutdown"
    }

    /* Why a step failed. */
    enum Reason: Equatable, Sendable {
        /* The server answered with a JSON-RPC error. */
        case serverError(code: Int, message: String)
        /* The server said nothing for this many seconds. */
        case timedOut(seconds: TimeInterval)
        /* The server's output ended, with the end of what it wrote to standard error. */
        case serverExited(standardError: String)
        /* The server could not be started, or an answer could not be read or written. */
        case unusable(String)
    }

    /* A step that did not complete, typed so a caller can report it as a named gap. */
    struct Failure: Error, CustomStringConvertible, Equatable {
        var step: Step
        var reason: Reason
        /* The server's last log messages before the failure, which often say what it was doing. */
        var recentLog: [String] = []

        var description: String {
            let what =
                switch reason {
                    case .serverError(let code, let message): "answered with error \(code): \(message)"
                    case .timedOut(let seconds): "gave no answer in \(Int(seconds))s"
                    case .serverExited(let standardError): "exited before answering: \(standardError)"
                    case .unusable(let detail): "could not be used: \(detail)"
                }
            let log = recentLog.isEmpty ? "" : " (its last log: \(recentLog.joined(separator: " / ")))"
            return "the build server's \(step.rawValue) \(what)\(log)"
        }
    }

    /* A target the server can build, named by the module its arguments compile when it has any. */
    struct Target: Equatable, Sendable {
        /* The server's identifier for it, a `swift-build://configured-target` URI. */
        var identifier: String
        var displayName: String
        var tags: [String]
        var dependencies: [String]
        var sources: [Source] = []
    }

    /* One file a target compiles. */
    struct Source: Equatable, Sendable {
        var url: URL
        /* As the server names it: `swift`, `c`, `cpp`, `objective-c`, `objective-cpp`; empty when it names none. */
        var language: String
        var isHeader: Bool
    }

    /* One target's compiler arguments for one language, asked of one of its files. */
    struct TargetOptions: Equatable, Sendable {
        var target: Target
        var language: String
        /* The file the arguments were asked for; a C file's arguments name it. */
        var askedFile: URL
        /* The compiler's arguments, without the compiler itself. */
        var arguments: [String]
        var workingDirectory: String?
    }

    /* What a whole session learned: where the server writes, its targets and their arguments. */
    struct Description: Sendable {
        /* `<scratch>/out`: the prepared modules under `Products/`, and the intermediates. */
        var outputPath: URL
        /* The index store the server names, under the scratch. Prepare writes nothing into it. */
        var indexStorePath: URL
        var targets: [Target]
        var options: [TargetOptions]
        /* What prepare did, nil when it was not asked to. */
        var prepared: Prepared?
    }

    /*
     What one prepare did: how long it took, and the plugin host tools it built and linked, the one kind of build a
     check allows (ruled on #nkk91tv): tools a build-tool plugin runs, never the project's code.
     */
    struct Prepared: Equatable, Sendable {
        var seconds: Double
        var toolsBuilt: [ToolBuilt]
    }

    /*
     One host tool prepare linked, named as its `Link <tool> (<architecture>)` log message names it, and the seconds
     from the first log message naming it to that link, which covers its own compile though not every dependency's.
     Measured 2026-11-06 on ahraos-presence: mlx-swift's `CudaBuild` plugin links `encuda`, after partial links of
     ArgumentParser's objects (`Link ArgumentParser.o`), which are part of the same tool and not counted apart.
     */
    struct ToolBuilt: Equatable, Sendable {
        var name: String
        var seconds: Double
    }

    /* Where `build/initialize` said the server writes. */
    struct Locations: Equatable, Sendable {
        var outputPath: URL
        var indexStorePath: URL
    }

    let configuration: Configuration
    private let process: Process
    private let input: FileHandle
    private let inbox: Inbox
    private let errorFile: URL
    private var nextIdentifier = 0
    private var finished = false

    /* Starts the server. Nothing is asked of it until `initialize`. */
    init(configuration: Configuration) throws(Failure) {
        self.configuration = configuration
        var arguments = ["swift", "package", "--package-path", configuration.packageRoot.path]
        arguments += ["--scratch-path", configuration.scratchPath.path]
        if let triple = configuration.triple {
            arguments += ["--triple", triple]
        }
        if let sdk = configuration.sdk {
            arguments += ["--sdk", sdk]
        }
        if !configuration.resolutionAllowed {
            arguments.append("--disable-automatic-resolution")
        }
        arguments.append("experimental-build-server")

        let inputPipe = Pipe()
        let outputPipe = Pipe()
        /* Standard error goes to a file, as `ProcessRunner`'s does: a pipe nobody reads fills at 64 KB and stalls the server. */
        errorFile = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-build-server-\(UUID().uuidString).err"
        )
        FileManager.default.createFile(atPath: errorFile.path, contents: nil)
        process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = arguments
        process.currentDirectoryURL = configuration.packageRoot
        process.environment = Self.environment(
            inheriting: configuration.inherited,
            workingDirectory: configuration.packageRoot,
        )
        process.standardInput = inputPipe
        process.standardOutput = outputPipe
        input = inputPipe.fileHandleForWriting
        /* A server that dies mid-request must fail the write, not end this process with `SIGPIPE`. */
        _ = fcntl(input.fileDescriptor, F_SETNOSIGPIPE, 1)
        inbox = Inbox()
        do {
            process.standardError = try FileHandle(forWritingTo: errorFile)
            try process.run()
        }
        catch {
            throw Failure(step: .launch, reason: .unusable("\(arguments.joined(separator: " ")): \(error)"))
        }
        Self.startReading(outputPipe.fileHandleForReading, into: inbox)
    }

    /*
     The server's environment: this process's, less what differs between two runs of the same check. SwiftPM hashes
     every variable into the key of each build-tool plugin's compile (`DefaultPluginScriptRunner` in 6.4.2), so one
     that changes every run compiles every such plugin again every run. Measured 2026-11-06 on ahraos-presence:
     mlx-swift's `CudaBuild` was compiled again on each check, 1.8s, though nothing had changed, because the front
     door's `COHERE_PROJECT_YIELD` (a fresh file each run) and `COHERE_SWIFT_RESOLVED_BY` (its pid) had. Cohere's own
     variables speak to cohere's processes and SwiftPM reads none of them; `OLDPWD` says only where a shell was
     before, and `PWD` is made the folder the server runs in.

     One variable is added, the same every run: Swift Build's `UseSynchronousBuildDescriptionSerialization`, which it
     reads from the environment as one of its defaults (#d1ynmrt). Without it, Swift Build reports a build description's
     identifier before it writes that description to disk, on a queue of the lowest priority
     (`BuildDescriptionManager.loadBuildDescription` in swift-build), and `workspace/buildTargets` asks for the
     description by that identifier, from memory and then from disk. When the copy in memory is gone, evicted under
     memory pressure or by its cost limit, the disk copy is not there yet, so the request fails with "No such file or
     directory (2)" before anything compiles, or with "Deserialization failed" when the file is half written, and the
     retries below fail the same way while the write waits. Measured 2026-11-07 with the copy in memory turned off
     (`BuildDescriptionInMemoryCacheSize` 0): 34 of 36 sessions failed so, and with this variable set, none of 36.
     */
    static func environment(inheriting inherited: [String: String], workingDirectory: URL) -> [String: String] {
        var environment = inherited.filter { !$0.key.hasPrefix("COHERE_") && $0.key != "OLDPWD" }
        environment["PWD"] = workingDirectory.path
        environment["UseSynchronousBuildDescriptionSerialization"] = "YES"
        return environment
    }

    deinit {
        if process.isRunning {
            process.terminate()
        }
        do {
            try FileManager.default.removeItem(at: errorFile)
        }
        catch {
            /* A leftover log costs a few bytes in a directory the system sweeps; nothing reads it again. */
        }
    }

    /*
     The whole session the check needs, in order: initialize, wait for the build description, list the buildable
     targets and their sources, ask each target's arguments of one file per language, prepare the targets `prepare`
     names once it has seen them and their arguments (all when it answers nil, none when empty), and shut down.
     `alongside`, when given, is started on another thread as soon as the targets and their arguments are known, with
     no prepare yet, and is waited for before this returns or throws, so work that needs no newly prepared module
     overlaps prepare.
     */
    static func describe(
        configuration: Configuration,
        prepare: (Description) -> Set<String>? = { _ in nil },
        alongside: (@Sendable (Description) -> Void)? = nil,
    ) throws(Failure) -> Description {
        let client = try BuildServerClient(configuration: configuration)
        let locations = try client.initialize()
        try client.waitForUpdates()
        let targets = try client.sources(of: client.buildTargets())
        var description = Description(
            outputPath: locations.outputPath,
            indexStorePath: locations.indexStorePath,
            targets: targets,
            options: try client.sourceKitOptions(for: targets),
        )
        let group = DispatchGroup()
        defer { group.wait() }
        if let alongside {
            let known = description
            /*
             A queue of its own, which always gets a thread: a caller on a Swift concurrency thread blocks on this group,
             and the global queue's threads are the ones such callers hold. Measured 2026-11-07 in the full test run,
             sixteen tests at once each blocked waiting on work queued on the global queue, and none of it started.
             */
            DispatchQueue(label: "cohere-swift.build-server.alongside").async(group: group) { alongside(known) }
        }
        let named = prepare(description)
        let preparing = targets.filter { named?.contains($0.displayName) ?? true }
        description.prepared = preparing.isEmpty ? nil : try client.prepare(preparing)
        try client.shutdown()
        return description
    }

    /*
     `build/initialize` and the `build/initialized` that must follow it. The locations it names are checked to sit
     under this run's scratch, so a server that would write anywhere else fails here, before it has built anything.
     */
    func initialize() throws(Failure) -> Locations {
        let parameters = InitializeParameters(
            rootUri: configuration.packageRoot.absoluteURL.absoluteString,
            capabilities: .init(languageIds: ["swift", "c", "cpp", "objective-c", "objective-cpp"]),
        )
        let result: InitializeResult = try request(.initialize, parameters)
        try notify("build/initialized", step: .initialize)
        guard let data = result.data, let output = data.indexDatabasePath, let store = data.indexStorePath else {
            throw Failure(
                step: .initialize,
                reason: .unusable("its answer names no indexDatabasePath or indexStorePath"),
            )
        }
        let locations = Locations(
            outputPath: URL(fileURLWithPath: output, isDirectory: true),
            indexStorePath: URL(fileURLWithPath: store, isDirectory: true),
        )
        for location in [locations.outputPath, locations.indexStorePath]
        where !Self.isInside(location, configuration.scratchPath) {
            throw Failure(
                step: .initialize,
                reason: .unusable(
                    "it would write to \(location.path), outside the scratch \(configuration.scratchPath.path)"
                ),
            )
        }
        return locations
    }

    /* Waits until the server has generated its build description, which the questions after this one read. */
    func waitForUpdates() throws(Failure) {
        let _: Ignored? = try request(.waitForUpdates, Empty())
    }

    /*
     Every target the server can build, the package manifest left out. None is a failure, never an empty package: a
     server whose build description could not be generated still answers, with the manifest alone, and says why only
     in its log and on standard error. Measured 2026-11-06 with a dependency and no Package.resolved under
     `--disable-automatic-resolution`: "Error generating build description", and SwiftPM's "a resolved file is
     required when automatic dependency resolution is disabled" on standard error.

     A description that failed once is generated again by the server itself, so the request is asked again after
     waiting for it, up to `attempts` times in all. Measured 2026-11-06 on ahraos-presence: right after a file was
     removed, the first description failed ("Error generating build description: No such file or directory (2)"),
     `workspace/buildTargets` answered -32001, and the description generated next was whole.
     */
    func buildTargets(attempts: Int = 3) throws(Failure) -> [Target] {
        var attempt = 1
        while true {
            do throws(Failure) {
                let result: BuildTargetsResult = try request(.buildTargets, Empty())
                let buildable = result.targets.filter { !$0.tags.contains("not-buildable") }
                if buildable.isEmpty {
                    throw failure(.buildTargets, .unusable("it lists no target it can build: \(standardErrorTail())"))
                }
                return Self.targets(from: buildable)
            }
            catch {
                /* A server that went silent or exited will not answer better the next time. */
                switch error.reason {
                    case .serverError, .unusable:
                        guard attempt < attempts else { throw error }
                    case .timedOut, .serverExited:
                        throw error
                }
                attempt += 1
                try waitForUpdates()
            }
        }
    }

    private static func targets(from buildable: [BuildTargetsResult.BuildTarget]) -> [Target] {
        buildable.map { target in
            Target(
                identifier: target.id.uri,
                displayName: target.displayName ?? target.id.uri,
                tags: target.tags,
                dependencies: target.dependencies.map(\.uri),
            )
        }
    }

    /* The targets with each one's files filled in, in the server's order. */
    func sources(of targets: [Target]) throws(Failure) -> [Target] {
        guard !targets.isEmpty else { return [] }
        let result: SourcesResult = try request(
            .sources,
            TargetsParameters(targets: targets.map { .init(uri: $0.identifier) }),
        )
        var byTarget: [String: [Source]] = [:]
        for item in result.items {
            byTarget[item.target.uri, default: []] += item.sources.compactMap { source in
                guard let url = URL(string: source.uri), url.isFileURL else { return nil }
                return Source(
                    url: url,
                    language: source.data?.language ?? "",
                    isHeader: source.data?.kind == "header",
                )
            }
        }
        return targets.map { target in
            var filled = target
            filled.sources = byTarget[target.identifier] ?? []
            return filled
        }
    }

    /* Each target's arguments for each language it compiles, asked of its first file in that language. */
    func sourceKitOptions(for targets: [Target]) throws(Failure) -> [TargetOptions] {
        var options: [TargetOptions] = []
        for target in targets {
            var asked = Set<String>()
            for source in target.sources where !source.isHeader && !source.language.isEmpty {
                guard asked.insert(source.language).inserted else { continue }
                let result: SourceKitOptionsResult = try request(
                    .sourceKitOptions,
                    SourceKitOptionsParameters(
                        textDocument: .init(uri: source.url.absoluteString),
                        target: .init(uri: target.identifier),
                        language: source.language,
                    ),
                )
                options.append(
                    TargetOptions(
                        target: target,
                        language: source.language,
                        askedFile: source.url,
                        arguments: result.compilerArguments,
                        workingDirectory: result.workingDirectory,
                    )
                )
            }
        }
        return options
    }

    /*
     Swift Build's prepare-for-indexing of these targets: their dependencies' modules, bodies skipped, no objects, and
     the host tools their build-tool plugins run, which it says it built.
     */
    func prepare(_ targets: [Target]) throws(Failure) -> Prepared {
        let start = Date()
        let logStart = inbox.logCount
        let _: Ignored? = try request(
            .prepare,
            TargetsParameters(targets: targets.map { .init(uri: $0.identifier) }),
            timeout: configuration.timeouts.prepare,
        )
        return Prepared(
            seconds: Date().timeIntervalSince(start),
            toolsBuilt: Self.toolsBuilt(in: inbox.log(from: logStart)),
        )
    }

    /* The host tools a prepare's log says it linked, in the order it linked them (`ToolBuilt`). */
    static func toolsBuilt(in log: [(arrived: Date, message: String)]) -> [ToolBuilt] {
        var tools: [ToolBuilt] = []
        for (index, entry) in log.enumerated() where entry.message.hasPrefix("Link ") {
            let linked = entry.message.dropFirst("Link ".count)
            let name = String(linked.split(separator: " (").first ?? linked)
            guard !name.hasSuffix(".o"), !name.isEmpty, !tools.contains(where: { $0.name == name }) else { continue }
            let first = log[...index].first { $0.message.contains(name) }?.arrived ?? entry.arrived
            tools.append(ToolBuilt(name: name, seconds: entry.arrived.timeIntervalSince(first)))
        }
        return tools
    }

    /* `build/shutdown`, then `build/exit`, then waits for the process to end. A server that does not end is stopped. */
    func shutdown() throws(Failure) {
        let _: Ignored? = try request(.shutdown, Empty())
        try notify("build/exit", step: .shutdown)
        finished = true
        let deadline = Date().addingTimeInterval(configuration.timeouts.exit)
        while process.isRunning, Date() < deadline {
            _ = inbox.wait(until: .now() + .milliseconds(100))
        }
        guard !process.isRunning else {
            process.terminate()
            throw Failure(step: .shutdown, reason: .timedOut(seconds: configuration.timeouts.exit))
        }
    }

    /*
     One request, answered or failed. The answer is decoded as `Answer`; a JSON-RPC error, an answer that cannot be
     decoded, the server's output ending, or silence past the timeout each fail as this step.
     */
    func request<Parameters: Encodable, Answer: Decodable>(
        _ step: Step,
        _ parameters: Parameters,
        timeout: TimeInterval? = nil,
    ) throws(Failure) -> Answer {
        nextIdentifier += 1
        let identifier = nextIdentifier
        try write(OutgoingRequest(id: identifier, method: step.rawValue, parameters: parameters), step: step)
        let seconds = timeout ?? configuration.timeouts.request
        let deadline = DispatchTime.now() + seconds
        while true {
            if let body = inbox.take(identifier) {
                let response: IncomingResponse<Answer>
                do {
                    response = try JSONDecoder().decode(IncomingResponse<Answer>.self, from: body)
                }
                catch {
                    throw failure(step, .unusable("its answer could not be read: \(error)"))
                }
                if let error = response.error {
                    throw failure(step, .serverError(code: error.code, message: error.message))
                }
                guard let result = response.result else {
                    throw failure(step, .unusable("its answer holds neither a result nor an error"))
                }
                return result
            }
            if let unreadable = inbox.unreadable {
                throw failure(step, .unusable("its output could not be read: \(unreadable)"))
            }
            if inbox.ended {
                process.waitUntilExit()
                throw failure(step, .serverExited(standardError: standardErrorTail()))
            }
            /* Looked at once more after the deadline passes, so an answer that arrived with it is still taken. */
            guard DispatchTime.now() < deadline else {
                throw failure(step, .timedOut(seconds: seconds))
            }
            _ = inbox.wait(until: deadline)
        }
    }

    private func notify(_ method: String, step: Step) throws(Failure) {
        try write(OutgoingNotification(method: method, parameters: Empty()), step: step)
    }

    /*
     Whether `location` sits under `scratch`, each compared as given and with symbolic links resolved, `/private` taken
     off. The index store does not exist until something writes it, Foundation resolves no link in a missing path, and
     it takes `/private` off an existing one, so `/var/folders/...` (a link to `/private/var/folders/...`) and its
     target must read as the same folder whichever of them exists.
     */
    static func isInside(_ location: URL, _ scratch: URL) -> Bool {
        func spellings(_ url: URL) -> Set<String> {
            Set(
                [url.standardizedFileURL.path, url.resolvingSymlinksInPath().path].map { path in
                    path.hasPrefix("/private/") ? String(path.dropFirst("/private".count)) : path
                }
            )
        }
        let scratches = spellings(scratch)
        return spellings(location).contains { path in scratches.contains { path.hasPrefix($0 + "/") } }
    }

    private func failure(_ step: Step, _ reason: Reason) -> Failure {
        Failure(step: step, reason: reason, recentLog: inbox.recentLog)
    }

    /* One message, framed as the Language Server Protocol frames it: a `Content-Length` header, a blank line, the JSON. */
    private func write<Message: Encodable>(_ message: Message, step: Step) throws(Failure) {
        do {
            let body = try JSONEncoder().encode(message)
            try input.write(contentsOf: Data("Content-Length: \(body.count)\r\n\r\n".utf8) + body)
        }
        catch {
            if inbox.ended {
                process.waitUntilExit()
                throw failure(step, .serverExited(standardError: standardErrorTail()))
            }
            throw failure(step, .unusable("the request could not be written: \(error)"))
        }
    }

    /* The last lines the server wrote to standard error, for a failure that ended it. */
    private func standardErrorTail() -> String {
        let text = (try? String(contentsOf: errorFile, encoding: .utf8)) ?? ""
        let lines = text.split(whereSeparator: \.isNewline).suffix(5)
        return lines.isEmpty ? "it wrote nothing to standard error" : lines.joined(separator: " / ")
    }

    /* Reads the server's output on a thread of its own, framing each message into the inbox until the output ends. */
    private static func startReading(_ output: FileHandle, into inbox: Inbox) {
        let thread = Thread {
            var framer = MessageFramer()
            while true {
                /*
                 Whatever has arrived, as soon as any has. `read(upToCount:)` waits on a pipe until the whole count or
                 the end has come, which held every short answer back until the request timed out.
                 */
                let chunk = output.availableData
                guard !chunk.isEmpty else {
                    inbox.end()
                    return
                }
                framer.append(chunk)
                do {
                    while let body = try framer.next() {
                        inbox.deliver(body)
                    }
                }
                catch {
                    inbox.fail("\(error)")
                    return
                }
            }
        }
        thread.start()
    }

    /*
     Splits a byte stream into the messages it frames. Headers end at a blank line; only `Content-Length` is read,
     and a header block without one is an error, since the stream cannot be followed past it.
     */
    struct MessageFramer {
        struct FramingError: Error, CustomStringConvertible {
            var description: String
        }

        private var buffer = Data()

        mutating func append(_ data: Data) {
            buffer.append(data)
        }

        /* The next whole message's body, or nil until one has arrived. */
        mutating func next() throws(FramingError) -> Data? {
            let separator = Data("\r\n\r\n".utf8)
            guard let headerEnd = buffer.range(of: separator) else { return nil }
            let header = String(decoding: buffer[buffer.startIndex..<headerEnd.lowerBound], as: UTF8.self)
            let length = header.split(separator: "\r\n").lazy.compactMap { line -> Int? in
                let parts = line.split(separator: ":", maxSplits: 1)
                guard parts.count == 2, parts[0].lowercased() == "content-length" else { return nil }
                return Int(parts[1].trimmingCharacters(in: .whitespaces))
            }
            .first
            guard let length else {
                throw FramingError(description: "a message header without a Content-Length: \(header)")
            }
            let bodyStart = headerEnd.upperBound
            guard buffer.distance(from: bodyStart, to: buffer.endIndex) >= length else { return nil }
            let bodyEnd = buffer.index(bodyStart, offsetBy: length)
            let body = Data(buffer[bodyStart..<bodyEnd])
            buffer = Data(buffer[bodyEnd...])
            return body
        }
    }

    /*
     What the reading thread has framed, held for the thread that asked: responses by identifier, the server's log
     messages with when each arrived, and whether its output has ended. Each arrival signals, and the asker looks again.
     A session's log is a few thousand short lines (4,864 for a cold prepare of ahraos-presence), kept whole so prepare
     can say what it built.
     */
    private final class Inbox: Sendable {
        private struct State {
            var responses: [Int: Data] = [:]
            var log: [(arrived: Date, message: String)] = []
            var ended = false
            var unreadable: String?
        }

        private struct Envelope: Decodable {
            var id: Int?
            var method: String?
            var parameters: LogParameters?

            enum CodingKeys: String, CodingKey {
                case id, method
                case parameters = "params"
            }
        }

        private struct LogParameters: Decodable {
            var message: String?
        }

        private let state = Mutex(State())
        private let arrived = DispatchSemaphore(value: 0)

        var recentLog: [String] { state.withLock { $0.log.suffix(5).map(\.message) } }
        var logCount: Int { state.withLock { $0.log.count } }

        /* Every log message since the `logCount` given, with when it arrived. */
        func log(from start: Int) -> [(arrived: Date, message: String)] {
            state.withLock { Array($0.log.dropFirst(start)) }
        }
        var ended: Bool { state.withLock { $0.ended } }
        var unreadable: String? { state.withLock { $0.unreadable } }

        /* One message: a response is kept for its asker, a log message is remembered, any other notification is dropped. */
        func deliver(_ body: Data) {
            guard let envelope = try? JSONDecoder().decode(Envelope.self, from: body) else { return }
            state.withLock { state in
                if let identifier = envelope.id, envelope.method == nil {
                    state.responses[identifier] = body
                }
                else if envelope.method == "build/logMessage", let message = envelope.parameters?.message {
                    state.log.append((Date(), message))
                }
            }
            arrived.signal()
        }

        func take(_ identifier: Int) -> Data? {
            state.withLock { $0.responses.removeValue(forKey: identifier) }
        }

        func end() {
            state.withLock { $0.ended = true }
            arrived.signal()
        }

        func fail(_ reason: String) {
            state.withLock {
                $0.unreadable = reason
                $0.ended = true
            }
            arrived.signal()
        }

        func wait(until deadline: DispatchTime) -> DispatchTimeoutResult {
            arrived.wait(timeout: deadline)
        }
    }

    /* The protocol's messages, spelled as the protocol spells them. */
    private struct OutgoingRequest<Parameters: Encodable>: Encodable {
        var jsonrpc = "2.0"
        var id: Int
        var method: String
        var parameters: Parameters

        enum CodingKeys: String, CodingKey {
            case jsonrpc, id, method
            case parameters = "params"
        }
    }

    private struct OutgoingNotification<Parameters: Encodable>: Encodable {
        var jsonrpc = "2.0"
        var method: String
        var parameters: Parameters

        enum CodingKeys: String, CodingKey {
            case jsonrpc, method
            case parameters = "params"
        }
    }

    private struct IncomingResponse<Answer: Decodable>: Decodable {
        var result: Answer?
        var error: ResponseError?
    }

    private struct ResponseError: Decodable {
        var code: Int
        var message: String
    }

    /* An answer read only for having arrived: `{}` or null. */
    struct Ignored: Decodable {}

    struct Empty: Codable {}

    private struct Identifier: Codable {
        var uri: String
    }

    private struct InitializeParameters: Encodable {
        struct Capabilities: Encodable {
            var languageIds: [String]
        }

        var displayName = "cohere-swift"
        var version = "1"
        var bspVersion = "2.2.0"
        var rootUri: String
        var capabilities: Capabilities
    }

    private struct InitializeResult: Decodable {
        struct SourceKitData: Decodable {
            var indexDatabasePath: String?
            var indexStorePath: String?
        }

        var data: SourceKitData?
    }

    private struct BuildTargetsResult: Decodable {
        struct BuildTarget: Decodable {
            var id: Identifier
            var displayName: String?
            var tags: [String]
            var dependencies: [Identifier]
        }

        var targets: [BuildTarget]
    }

    private struct TargetsParameters: Encodable {
        var targets: [Identifier]
    }

    private struct SourcesResult: Decodable {
        struct Item: Decodable {
            var target: Identifier
            var sources: [SourceItem]
        }

        struct SourceItem: Decodable {
            struct SourceData: Decodable {
                var language: String?
                var kind: String?
            }

            var uri: String
            var data: SourceData?
        }

        var items: [Item]
    }

    private struct SourceKitOptionsParameters: Encodable {
        var textDocument: Identifier
        var target: Identifier
        var language: String
    }

    private struct SourceKitOptionsResult: Decodable {
        var compilerArguments: [String]
        var workingDirectory: String?
    }
}
