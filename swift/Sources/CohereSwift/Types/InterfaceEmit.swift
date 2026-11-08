import Foundation
import Synchronization

/*
 The interface of an edited module, emitted by the check itself for the scoped modules that import it, so prepare
 need not emit it (#da5fx6d, ruled 2026-11-07). Prepare emitted an owned module twice, the testable variant and the
 product's, one after the other, and every test product depends on both, so no choice of targets asks it for one.
 The check reads only the testable one: `Products/<configuration>/<Module>.swiftmodule` is the testable variant's
 copy, byte for byte, and the product's is read by no check job. Measured 2026-11-07 on ahraos-presence's interface
 edit, main's engine: the two emits ended 21.5s into the quietest run and the test modules waited for the second;
 the testable emit alone took 6.1s, and 11.8s beside the owned module's own check.

 The emit is the one prepare's testable variant runs: the build server's testable arguments for the module, its
 prepare flags kept and `-experimental-skip-all-function-bodies` added as Swift Build adds it, as the driver plans
 an `-emit-module` job, without the `-experimental-skip-non-inlinable-function-bodies-without-types` the driver adds
 on its own. It is not byte for byte prepare's: the debugging options it serializes (search paths, Clang
 arguments) are spelled as the server gave them, which Swift Build spells otherwise. Each module goes into a folder
 of its own under `<scratch>/check/interfaces`, which each module the check reads it through gets as its first
 search path.

 The `Products` copy of a module the check emitted is older than its sources until a prepare emits it again, so the
 ledger (`Ledger`) names every such module, and every module checked after takes the check's copy through its
 search path until a prepare of everything clears it.
 */
struct InterfaceEmit {
    /* `swiftc` from the toolchain the check uses, which plans the emit. */
    let swiftc: String
    /* Where the driver and the frontend run, as for the check's own jobs. */
    let workingDirectory: URL
    var runner = ProcessRunner()

    /*
     What a narrowed prepare left older in `Products` than its sources, kept beside the interfaces until a prepare of
     everything. `targets` are the owned modules, by target, whose current interface is in their folder here, since
     the check emitted it; one edited again is in the scope again, and emitted again (`TypesPhase`). `stale` are the
     scoped modules the check did not emit, since nothing it checked imported them, so no current copy exists
     anywhere: a check that would read one prepares everything (cache's review of 04d698c1f). A scratch without the
     file, an older engine's or one a failed emit left, cannot say which copies are older, so prepare emits everything
     and the file starts empty.
     */
    struct Ledger: Codable, Equatable {
        var targets: [String]
        var stale: [String] = []

        static func directory(scratchPath: URL) -> URL {
            TypesPhase.checkDirectory(scratchPath: scratchPath).appendingPathComponent("interfaces", isDirectory: true)
        }

        static func file(scratchPath: URL) -> URL {
            directory(scratchPath: scratchPath).appendingPathComponent("emitted.json")
        }

        /* The folder one target's emitted interface lives in, `<scratch>/check/interfaces/<Target>`. */
        static func moduleDirectory(scratchPath: URL, target: String) -> URL {
            directory(scratchPath: scratchPath).appendingPathComponent(target, isDirectory: true)
        }

        /* The ledger this scratch keeps, nil when there is none it can read. */
        static func stored(scratchPath: URL) -> Ledger? {
            do {
                return try JSONDecoder().decode(Ledger.self, from: Data(contentsOf: file(scratchPath: scratchPath)))
            }
            catch {
                /* None to go by, so prepare emits everything. */
                return nil
            }
        }

        func store(scratchPath: URL) throws {
            try FileManager.default.createDirectory(
                at: Self.directory(scratchPath: scratchPath),
                withIntermediateDirectories: true,
            )
            try JSONEncoder().encode(self).write(to: Self.file(scratchPath: scratchPath), options: .atomic)
        }

        /*
         The lock a run holds on the interfaces from reading the ledger to the end of its check, or of its in-process
         check of bodies, so a second run on the same root waits: one run clearing a folder before its emit would
         otherwise send the other's importers to the older `Products` copy, and two ledger writes would lose one
         (cache's review of 04d698c1f). An exclusive `flock`, which the kernel drops however the holder ends, as
         TypeScript's `table.lock` does. Nil when the scratch cannot hold one, and then nothing reads the ledger.
         Sendable, since it holds only its descriptor: the in-process check carries it to the queue it asks on.
         */
        final class Lock: Sendable {
            private let descriptor: Int32

            init?(scratchPath: URL) {
                let directory = Ledger.directory(scratchPath: scratchPath)
                do {
                    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
                }
                catch {
                    return nil
                }
                let opened = unsafe open(directory.appendingPathComponent("lock").path, O_CREAT | O_RDWR, 0o644)
                guard opened >= 0 else { return nil }
                guard flock(opened, LOCK_EX) == 0 else {
                    close(opened)
                    return nil
                }
                descriptor = opened
            }

            deinit {
                close(descriptor)
            }
        }

        /* Removed when an emit failed, so the next check prepares everything. One already gone is what was wanted. */
        static func forget(scratchPath: URL) {
            do {
                try FileManager.default.removeItem(at: file(scratchPath: scratchPath))
            }
            catch {
                /* No file to remove, or an unwritable scratch that a later check cannot write a ledger into either. */
            }
        }
    }

    /*
     Called with each folder right after its earlier interface is removed and before the emit writes the new one: the
     seam a test pauses one run at, to show a second run on the same scratch waits for it rather than read the folder
     empty. Nil, and so nothing, outside that test.
     */
    static let cleared = Mutex<(@Sendable (URL) -> Void)?>(nil)

    /* The files an emit writes beside the module, each named by the module. */
    static let outputExtensions = ["swiftmodule", "swiftdoc", "swiftsourceinfo", "abi.json"]

    /*
     One module's emit, planned and its folder made ready, to be run once prepare is done. The driver plans while
     prepare runs, since planning reads no module: measured 2026-11-07 on ahraos-presence, planning after prepare left
     the frontend starting 3.2s after the build server's session ended, beside the owned module's check.
     */
    struct Planned: Sendable {
        var moduleName: String
        var interface: URL
        var job: Job
    }

    /*
     One module's interface into `directory`, searching `searchPaths` first, so a module it imports that the check
     emitted is read from there. The folder's earlier interface is removed first, so an emit that fails leaves no
     interface, never an older one.
     */
    func planned(
        moduleName: String,
        arguments: [String],
        into directory: URL,
        searchPaths: [URL],
    ) throws(FrontendCheck.Failure) -> Planned {
        let interface = directory.appendingPathComponent("\(moduleName).swiftmodule")
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            for pathExtension in Self.outputExtensions {
                try FrontendCheck.removeRecord(directory.appendingPathComponent("\(moduleName).\(pathExtension)"))
            }
        }
        catch {
            throw FrontendCheck.Failure(
                module: moduleName,
                detail: "its interface's folder could not be made ready: \(error)",
            )
        }
        Self.cleared.withLock { $0 }?(directory)
        return Planned(
            moduleName: moduleName,
            interface: interface,
            job: try plan(
                moduleName: moduleName,
                arguments: arguments,
                interface: interface,
                searchPaths: searchPaths,
            ),
        )
    }

    /*
     The planned emit, run. With errors in the module the frontend still writes it and exits with a failure, as
     prepare's does (`-experimental-allow-module-with-compiler-errors`), and the check reports the errors; an emit that
     wrote no interface fails.
     */
    func run(_ planned: Planned) throws(FrontendCheck.Failure) {
        let result: ProcessRunner.Result
        do {
            result = try runner.run(planned.job.executable, planned.job.arguments, in: workingDirectory)
        }
        catch {
            throw FrontendCheck.Failure(
                module: planned.moduleName,
                detail: "its interface's emit could not be run: \(error)",
            )
        }
        guard FileManager.default.fileExists(atPath: planned.interface.path) else {
            throw FrontendCheck.Failure(
                module: planned.moduleName,
                detail: "its interface's emit wrote none: \(FrontendCheck.tail(result.standardError))",
            )
        }
    }

    /* The frontend job the driver plans for the emit, as the check runs it. */
    struct Job: Equatable, Sendable {
        var executable: String
        var arguments: [String]
    }

    func plan(
        moduleName: String,
        arguments: [String],
        interface: URL,
        searchPaths: [URL],
    ) throws(FrontendCheck.Failure) -> Job {
        let result: ProcessRunner.Result
        do {
            result = try runner.run(
                swiftc,
                Self.driverArguments(arguments, interface: interface, searchPaths: searchPaths),
                in: workingDirectory,
            )
        }
        catch {
            throw FrontendCheck.Failure(module: moduleName, detail: "the driver could not be started: \(error)")
        }
        guard result.succeeded else {
            throw FrontendCheck.Failure(
                module: moduleName,
                detail: "the driver could not plan its interface's emit: \(FrontendCheck.tail(result.standardError))",
            )
        }
        /*
         The plan prints every compile job of the module too, each with every file inline: 105 MB for ahraos-presence's
         744 files (measured 2026-11-07), which splitting into words took longer than the emit's frontend. Only the line
         that names the emit is split.
         */
        let marker = Data(" -emit-module ".utf8)
        for line in result.standardOutput.split(separator: UInt8(ascii: "\n")) where line.range(of: marker) != nil {
            let words = FrontendCheck.shellWords(String(decoding: line, as: UTF8.self))
            guard words.count > 1, words[1] == "-frontend", words.contains("-emit-module"), let executable = words.first
            else { continue }
            return Job(
                executable: executable,
                arguments: words.dropFirst().filter { $0 != Self.driverSkipFlag },
            )
        }
        throw FrontendCheck.Failure(
            module: moduleName,
            detail: "the driver planned no -emit-module job for its interface",
        )
    }

    /* The flag the driver adds to an emit-module job of its own accord, which prepare's emit does not carry. */
    static let driverSkipFlag = "-experimental-skip-non-inlinable-function-bodies-without-types"

    /*
     The driver's arguments for planning the emit: the server's, outputs and planning flags removed and prepare's flags
     kept, the search paths first, then the interface to write and prepare's skipped bodies.
     */
    static func driverArguments(_ arguments: [String], interface: URL, searchPaths: [URL]) -> [String] {
        let cleaned = FrontendCheck.cleaned(arguments, keepingPrepareFlags: true)
        let skip = "-experimental-skip-all-function-bodies"
        let skipping = zip(cleaned, cleaned.dropFirst()).contains { $0 == "-Xfrontend" && $1 == skip }
        return searchPaths.flatMap { ["-I", $0.path] } + cleaned
            + ["-emit-module", "-emit-module-path", interface.path]
            + (skipping ? [] : ["-Xfrontend", skip])
            + ["-driver-print-jobs", "-driver-filelist-threshold=\(Int32.max)"]
    }

    /* Whether these arguments build modules explicitly, from a map of modules rather than any search path. */
    static func buildsModulesExplicitly(_ arguments: [String]) -> Bool {
        arguments.contains { ["-explicit-module-build", "-explicit-swift-module-map-file"].contains($0) }
    }

    /* The search path arguments that put these folders before every other, for a module's check or emit. */
    static func searchingFirst(_ directories: [URL], in arguments: [String]) -> [String] {
        directories.flatMap { ["-I", $0.path] } + arguments
    }

    /*
     The arguments prepare's testable variant compiled a module with: the server offers an executable's module twice,
     once testable for its tests (`-entry-point-function-name`), and that one is what the tests import. A library has
     one set, and it is that.
     */
    static func testableArguments(
        of module: FrontendCheck.Module,
        options: [BuildServerClient.TargetOptions],
    ) -> [String] {
        guard let name = FrontendCheck.Module(arguments: module.arguments)?.name else { return module.arguments }
        let testable = options.first { option in
            option.language == "swift" && option.arguments.contains("-entry-point-function-name")
                && FrontendCheck.Module(arguments: option.arguments)?.name == name
        }
        return testable?.arguments ?? module.arguments
    }

    /*
     The targets in the order their interfaces can be emitted, each after every one of them it imports, by name and
     then by the order imports allow. A cycle, which no package graph has, keeps the rest in name order.
     */
    static func emitOrder(_ targets: Set<String>, imports: [String: Set<String>]) -> [String] {
        var ordered: [String] = []
        var remaining = targets.sorted()
        while !remaining.isEmpty {
            let placed = Set(ordered.map(TypesPhase.moduleName(of:)))
            let pending = Set(remaining.map(TypesPhase.moduleName(of:)))
            let ready =
                remaining.first { target in
                    imports[target, default: []].subtracting([TypesPhase.moduleName(of: target)]).subtracting(placed)
                        .isDisjoint(with: pending)
                } ?? remaining[0]
            ordered.append(ready)
            remaining.removeAll { $0 == ready }
        }
        return ordered
    }
}
