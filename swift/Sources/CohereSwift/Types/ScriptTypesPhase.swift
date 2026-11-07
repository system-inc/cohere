import Foundation

/*
 The compiler's own errors and warnings for each standalone script of ours (`StandaloneScript`), each checked alone, as
 `swift <file>` runs it, in a module of its own (#13nbb1s).

 The check is a package's (`FrontendCheck`), so a script is judged as a target's file is: the driver plans its one job
 as `-typecheck`, which runs as `-emit-sib` to /dev/null, so the passes after type checking (missing returns, overflow,
 use before initialization, data races) report as a build would, with a serialized diagnostics record and an index
 unit for the script. Both go under the scripts' own scratch, `<scratch>/scripts`, laid out as a package's check lays
 its out, so `CompilerRecords` reads the records, and the typed rules and `--unused` read the index and the compile
 arguments (`CompileCommands`) as they read a package's (`Pipeline.symbolLayouts`). Nothing is reused from an earlier
 run: a script is one job, checked again every run, and the front door replays a whole verdict while nothing the run
 read has moved.

 An error in a script says it was checked alone, since a script that needs companion files fails exactly that way and
 the fix is a package. A script whose SDK this Xcode does not have has no record, which makes the run incomplete, and
 the words say why.
 */
struct ScriptTypesPhase {
    let scripts: [FileSet.OwnedFile]
    /* The run's scratch, which the scripts' own is under (`scratchPath(in:)`). */
    let scratchPath: URL
    /* Where the driver and the frontends run, the run's root: every path they are given is absolute. */
    let workingDirectory: URL
    let runner: ProcessRunner

    /* What the check found, which scripts it could not vouch for, and the words for the types record. */
    struct Result {
        var findings: [FindingRecord]
        var filesWithoutRecord: [String]
        var words: String
        var elapsedMilliseconds: Int
    }

    /* The scripts' scratch under a run's: a check layout of its own, `.check(scratchPath:)`, beside the package's. */
    static func scratchPath(in scratch: URL) -> URL {
        scratch.appendingPathComponent("scripts", isDirectory: true)
    }

    /* Said after the compiler's own message on each error in a script. */
    static func checkedAloneWords(_ script: URL) -> String {
        " (checked alone, as `swift \(script.lastPathComponent)` runs it: a script that needs other files to compile belongs in a package with them)"
    }

    /* The SDK each platform is checked against, as xcrun names it. */
    static let sdkNames: [StandaloneScript.Platform: String] = [.macOS: "macosx", .iOS: "iphoneos"]

    /* Where a platform's scripts are checked: its SDK, and the triple when it is not the host's. */
    struct Destination {
        var sdk: String
        var target: String?
    }

    func run() throws -> Result {
        let start = Date()
        let swiftc = try FrontendCheck.toolchainExecutable(named: "swiftc", runner: runner)
        /* Each platform's destination found once, and only for a platform a script is checked for. */
        var destinations: [StandaloneScript.Platform: Swift.Result<Destination, Unchecked>] = [:]
        for platform in Set(scripts.compactMap(\.scriptPlatform)) {
            destinations[platform] = destination(for: platform)
        }
        var modules: [FrontendCheck.Module] = []
        var checked: [FileSet.OwnedFile] = []
        var unchecked: [(file: String, reason: String)] = []
        for script in scripts {
            guard let platform = script.scriptPlatform, let found = destinations[platform] else { continue }
            let destination: Destination
            switch found {
                case .success(let ready):
                    destination = ready
                case .failure(let reason):
                    unchecked.append((script.url.path, reason.description))
                    continue
            }
            modules.append(
                FrontendCheck.Module(
                    name: script.targetName,
                    arguments: StandaloneScript.arguments(
                        module: script.targetName,
                        file: script.url,
                        sdk: destination.sdk,
                        target: destination.target,
                    ),
                )
            )
            checked.append(script)
        }

        let scratch = Self.scratchPath(in: scratchPath)
        var findings: [FindingRecord] = []
        var withoutRecord = unchecked.map(\.file)
        var failure = ""
        if !modules.isEmpty {
            let frontend = FrontendCheck(
                swiftc: swiftc,
                diagnosticsRoot: TypesPhase.checkDirectory(scratchPath: scratch),
                indexStorePath: IndexStore.storePath(layout: .check(scratchPath: scratch)),
                workingDirectory: workingDirectory,
                runner: runner,
            )
            let outcome: FrontendCheck.Outcome?
            do throws(FrontendCheck.Failure) {
                outcome = try frontend.check(modules)
            }
            catch {
                failure = "; not checked, since \(error)"
                outcome = nil
            }
            if let outcome {
                CompileCommands(commands: modules.compactMap(CompileCommands.command(checking:)))
                    .store(scratchPath: scratch)
                let reading = try CompilerRecords(files: checked, runner: runner).read(
                    targetDirectories: outcome.targetDirectories
                )
                findings = reading.findings.map { finding in
                    guard finding.severity == .error else { return finding }
                    var alone = finding
                    alone.message += Self.checkedAloneWords(URL(fileURLWithPath: finding.file))
                    return alone
                }
                findings += outcome.crashes.map(TypesPhase.finding(for:))
                /* A job that failed with no error in its script failed somewhere no file of ours carries: said at the script, never swallowed. */
                for failed in outcome.failedJobs {
                    for primary in failed.primaries {
                        let file = URL(fileURLWithPath: primary).resolvingSymlinksInPath().path
                        guard !findings.contains(where: { $0.file == file && $0.severity == .error }) else { continue }
                        findings.append(
                            FindingRecord(
                                source: .compiler,
                                file: file,
                                line: 1,
                                column: 1,
                                severity: .error,
                                rule: "",
                                messageId: "buildFailed",
                                message:
                                    "the check of this script failed with no error placed in it: \(failed.standardError)",
                            )
                        )
                    }
                }
                withoutRecord += reading.filesWithoutRecord
            }
            else {
                withoutRecord += checked.map(\.url.path)
            }
        }

        findings.sort { ($0.file, $0.line, $0.column) < ($1.file, $1.line, $1.column) }
        let counts = Dictionary(grouping: checked, by: { $0.scriptPlatform.map { "\($0)" } ?? "" }).mapValues(\.count)
        var words =
            "\(checked.count == 1 ? "1 script" : "\(checked.count) scripts") checked alone with -emit-sib, each in a module of its own, as `swift <file>` runs it"
        let platforms = counts.keys.sorted().map { "\(counts[$0] ?? 0) for \($0)" }
        if !platforms.isEmpty {
            words += " (\(platforms.joined(separator: ", ")))"
        }
        words += failure
        for (file, reason) in unchecked {
            words += "; \(file) not checked: \(reason)"
        }
        return Result(
            findings: findings,
            filesWithoutRecord: withoutRecord,
            words: words,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
        )
    }

    /* Why a platform's scripts could not be checked. */
    struct Unchecked: Error, CustomStringConvertible {
        var description: String
    }

    /*
     Where a platform's scripts are checked. macOS is the host, which the driver targets at its own version, as it does
     for `swift <file>`. iOS is the iOS SDK with an arm64 device triple at the SDK's own default deployment target, its
     `SDKSettings.json`'s `DefaultDeploymentTarget`, what Xcode gives a new target: a triple with no version is iOS's
     oldest, and measured 2026-11-06 on Swift 6.4.2 it called `UIColor.systemBackground` "only available in iOS 13.0 or
     newer", a false error no script's author would ever see.
     */
    private func destination(for platform: StandaloneScript.Platform) -> Swift.Result<Destination, Unchecked> {
        let sdkName = Self.sdkNames[platform] ?? ""
        guard let result = try? runner.run("xcrun", ["--sdk", sdkName, "--show-sdk-path"], in: workingDirectory),
            result.succeeded,
            case let sdk = String(decoding: result.standardOutput, as: UTF8.self).trimmingCharacters(
                in: .whitespacesAndNewlines
            ),
            !sdk.isEmpty
        else {
            return .failure(Unchecked(description: "this Xcode has no \(sdkName) SDK to check it for \(platform)"))
        }
        guard platform == .iOS else { return .success(Destination(sdk: sdk, target: nil)) }
        let settings = URL(fileURLWithPath: sdk).appendingPathComponent("SDKSettings.json")
        guard let data = FileManager.default.contents(atPath: settings.path),
            let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let deployment = object["DefaultDeploymentTarget"] as? String, !deployment.isEmpty
        else {
            return .failure(
                Unchecked(
                    description: "\(settings.path) names no DefaultDeploymentTarget to check it for \(platform) at"
                )
            )
        }
        return .success(Destination(sdk: sdk, target: "arm64-apple-ios\(deployment)"))
    }

    /* The types phase's result with the scripts' added: their findings in order among the targets', their files counted, their words after. */
    static func adding(_ scripts: Result, count: Int, to targets: TypesPhase.Result) -> TypesPhase.Result {
        var result = targets
        result.findings += scripts.findings
        result.findings.sort { ($0.file, $0.line, $0.column) < ($1.file, $1.line, $1.column) }
        result.record.diagnostics = result.findings.count
        result.record.files += count
        result.record.filesWithoutRecord = (result.record.filesWithoutRecord + scripts.filesWithoutRecord).sorted()
        result.record.elapsedMilliseconds += scripts.elapsedMilliseconds
        result.record.build += "; \(scripts.words)"
        result.hasErrors = result.findings.contains { $0.severity == .error }
        return result
    }
}
