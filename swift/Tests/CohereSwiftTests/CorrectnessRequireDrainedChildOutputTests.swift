import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 `cohere-swift/correctness-require-drained-child-output` both ways, with the fixtures #pte6ec9 gives every engine:
 Servers.app's CPU sampler as it stood before servers-macos 5c5833e (red) and `HelperCommand` as that commit wrote
 it (green), a drain on one branch (red), a concurrent drain (green), a discarded or inherited stream (green), and
 a stderr drain that leaves stdout's pipe unread (red). Then every path shape the walk lays out, and every shape
 that falls to silence because the drain or the wait cannot be placed. Each red case asserts where its finding
 starts and which pipe it names, so a finding at the wrong wait fails rather than passing on its count.
 */
struct CorrectnessRequireDrainedChildOutputTests {
    static func file(_ source: String) -> ParsedFile {
        ParsedFile(
            url: URL(fileURLWithPath: "/fixture/Subject.swift"),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
        )
    }

    static func findings(_ source: String) -> [FindingRecord] {
        let file = Self.file(source)
        let rule = CorrectnessRequireDrainedChildOutput()
        let found = rule.findings(in: file)
        #expect(found.isEmpty || rule.applies(to: file), "the prefilter must never hide a finding")
        #expect(found.allSatisfy { $0.fixes.isEmpty && $0.suggestions.isEmpty }, "the rule never fixes")
        guard rule.applies(to: file) else { return [] }
        return found
    }

    /* Each finding as `line:column pipe`, the pipe read back from the message's opening. */
    static func sites(_ source: String) -> [String] {
        findings(source).map { finding in
            let pipe = finding.message.split(separator: "`").first.map(String.init) ?? ""
            return "\(finding.line):\(finding.column) \(pipe)"
        }
    }

    /* A body of code inside a function, so each case reads as the statements it is about. */
    static func function(_ body: String) -> String {
        "func sample(verbose: Bool, mode: Int) throws -> Data {\n" + body + "\n}\n"
    }

    // MARK: The outage

    /* Servers.app's `ServerManager.sampleCpuUsage`, the closure it dispatched, at 5c5833e's parent. */
    static let oldSampler = """
        private func sampleCpuUsage() {
            DispatchQueue.global(qos: .utility).async { [weak self] in
                guard let self = self else { return }

                // Get all processes with pid, ppid, %cpu
                let task = Process()
                task.executableURL = URL(fileURLWithPath: "/bin/ps")
                task.arguments = ["-eo", "pid,ppid,%cpu"]
                let pipe = Pipe()
                task.standardOutput = pipe
                task.standardError = FileHandle.nullDevice

                do {
                    try task.run()
                    task.waitUntilExit()
                } catch {
                    return
                }

                let data = pipe.fileHandleForReading.readDataToEndOfFile()
                guard let output = String(data: data, encoding: .utf8) else { return }

                // Parse ps output into (pid, ppid, cpu) tuples
                struct PsEntry {
                    let pid: pid_t
                    let ppid: pid_t
                    let cpu: Double
                }
                self.parse(output)
            }
        }
        """

    @Test func theOldSamplerIsReportedAtItsWait() {
        let found = Self.findings(Self.oldSampler)
        #expect(found.map { "\($0.line):\($0.column)" } == ["15:13"])
        #expect(
            found.first?.message
                == "`pipe`, the pipe `task` writes its standard output to, is not read to its end on some path before `task.waitUntilExit()`, so output past the pipe buffer blocks the child and the wait never returns. Read it to its end before the wait (`pipe.fileHandleForReading.readDataToEndOfFile()`), or drain it with a `readabilityHandler` set before the wait, or give the stream `FileHandle.nullDevice` when nothing needs it."
        )
        #expect(found.first?.messageId == "childOutputNeverDrained")
        #expect(found.first?.rule == "cohere-swift/correctness-require-drained-child-output")
    }

    /* servers-macos 5c5833e's `Servers/HelperCommand.swift`, verbatim. */
    static let helperCommand = """
        import Foundation

        enum HelperCommand {
            static func output(_ executablePath: String, arguments: [String], timeout: TimeInterval = 5) -> Data? {
                let process = Process()
                process.executableURL = URL(fileURLWithPath: executablePath)
                process.arguments = arguments
                process.standardInput = FileHandle.nullDevice
                process.standardError = FileHandle.nullDevice
                let pipe = Pipe()
                process.standardOutput = pipe

                do {
                    try process.run()
                }
                catch {
                    return nil
                }

                let timedOutLock = NSLock()
                var timedOut = false
                let watchdog = DispatchWorkItem {
                    // Still running means not yet reaped, so the pid can't have been reused.
                    guard process.isRunning else { return }
                    timedOutLock.lock()
                    timedOut = true
                    timedOutLock.unlock()
                    kill(process.processIdentifier, SIGKILL)
                }
                DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout, execute: watchdog)

                // Drain first: returns at end of file, when the child exits or the watchdog kills it.
                let data = pipe.fileHandleForReading.readDataToEndOfFile()
                do {
                    try pipe.fileHandleForReading.close()
                }
                catch {
                    // The output is already read whole; a handle that won't close is released with the pipe.
                }
                process.waitUntilExit()
                watchdog.cancel()

                timedOutLock.lock()
                defer { timedOutLock.unlock() }
                return timedOut ? nil : data
            }
        }
        """

    @Test func theFixedHelperIsClean() {
        #expect(Self.sites(Self.helperCommand) == [])
    }

    /* The fixed helper with its drain moved after the wait: the same body, now the outage's order. */
    @Test func theFixedHelperWaitingFirstIsReported() {
        let waitingFirst = Self.helperCommand
            .replacingOccurrences(
                of: "        let data = pipe.fileHandleForReading.readDataToEndOfFile()\n",
                with: "        process.waitUntilExit()\n",
            )
            .replacingOccurrences(
                of: "        }\n        process.waitUntilExit()\n        watchdog.cancel()\n",
                with: "        }\n        let data = pipe.fileHandleForReading.readDataToEndOfFile()\n"
                    + "        watchdog.cancel()\n",
            )
        #expect(waitingFirst != Self.helperCommand)
        #expect(Self.sites(waitingFirst) == ["33:9 pipe"])
    }

    // MARK: Paths

    static let launched = """
        let process = Process()
        let pipe = Pipe()
        process.standardOutput = pipe
        try process.run()
        """

    @Test func aDrainOnOneBranchIsReported() {
        let source = Self.function(
            Self.launched + """

                var output = Data()
                if verbose {
                    output = pipe.fileHandleForReading.readDataToEndOfFile()
                }
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(source) == ["10:1 pipe"])
    }

    /* From the review of a5ec7d71f: one branch attaches the pipe and drains it, and the other never attaches it. */
    @Test func aPipeAttachedAndDrainedOnOneBranchAndNeverOnTheOtherIsClean() {
        let source = Self.function(
            """
            let process = Process()
            let pipe = Pipe()
            if verbose {
                process.standardOutput = pipe
                try process.run()
                _ = pipe.fileHandleForReading.readDataToEndOfFile()
            } else {
                try process.run()
            }
            process.waitUntilExit()
            return Data()
            """
        )
        #expect(Self.sites(source) == [])
    }

    /* Its mirror: the attach before the branching, the drain on the verbose branch only. */
    @Test func aPipeAttachedBeforeTheBranchingAndDrainedOnOneIsReported() {
        let source = Self.function(
            """
            let process = Process()
            let pipe = Pipe()
            process.standardOutput = pipe
            if verbose {
                try process.run()
                _ = pipe.fileHandleForReading.readDataToEndOfFile()
            } else {
                try process.run()
            }
            process.waitUntilExit()
            return Data()
            """
        )
        #expect(Self.sites(source) == ["11:1 pipe"])
    }

    @Test func aDrainOnTheElseBranchOnlyIsReported() {
        let source = Self.function(
            Self.launched + """

                var output = Data()
                if verbose {
                    output = Data()
                } else if mode == 1 {
                    output = pipe.fileHandleForReading.readDataToEndOfFile()
                } else {
                    output = try pipe.fileHandleForReading.readToEnd() ?? Data()
                }
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(source) == ["14:1 pipe"])
    }

    @Test func aDrainOnEveryBranchIsClean() {
        let source = Self.function(
            Self.launched + """

                var output = Data()
                if verbose {
                    output = pipe.fileHandleForReading.readDataToEndOfFile()
                } else {
                    output = try pipe.fileHandleForReading.readToEnd() ?? Data()
                }
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aDrainBeforeTheBranchingIsClean() {
        let source = Self.function(
            Self.launched + """

                let output = pipe.fileHandleForReading.readDataToEndOfFile()
                if verbose {
                    process.waitUntilExit()
                } else {
                    process.waitUntilExit()
                }
                return output
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aBranchThatLeavesDoesNotWeakenTheJoin() {
        let source = Self.function(
            Self.launched + """

                if verbose {
                    _ = pipe.fileHandleForReading.readDataToEndOfFile()
                } else if mode == 0 {
                    return Data()
                } else {
                    fatalError("the mode is checked before the call")
                }
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aWaitOnAnEarlyReturnPathIsReported() {
        let source = Self.function(
            Self.launched + """

                guard verbose else {
                    process.waitUntilExit()
                    return Data()
                }
                let output = pipe.fileHandleForReading.readDataToEndOfFile()
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(source) == ["7:5 pipe"])
    }

    @Test func aGuardWhoseFirstConditionDrainsIsClean() {
        let source = Self.function(
            Self.launched + """

                guard let output = try pipe.fileHandleForReading.readToEnd(), !output.isEmpty else {
                    process.waitUntilExit()
                    return Data()
                }
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aDrainInALaterConditionCountsOnlyWhereItPassed() {
        let source = Self.function(
            Self.launched + """

                if verbose, let output = try pipe.fileHandleForReading.readToEnd() {
                    process.waitUntilExit()
                    return output
                }
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(source) == ["10:1 pipe"])
    }

    @Test func aSwitchDrainedInOneCaseIsReported() {
        let source = Self.function(
            Self.launched + """

                switch mode {
                    case 0:
                        _ = pipe.fileHandleForReading.readDataToEndOfFile()
                    case 1:
                        break
                    default:
                        _ = pipe.fileHandleForReading.readDataToEndOfFile()
                }
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(source) == ["14:1 pipe"])
    }

    @Test func aSwitchDrainedInEveryCaseIsClean() {
        let source = Self.function(
            Self.launched + """

                switch mode {
                    case 0:
                        _ = pipe.fileHandleForReading.readDataToEndOfFile()
                    case 1:
                        guard verbose else {
                            pipe.fileHandleForReading.readabilityHandler = { handle in _ = handle.availableData }
                            break
                        }
                        _ = pipe.fileHandleForReading.readDataToEndOfFile()
                    default:
                        return Data()
                }
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aCatchStartsFromWhatTheDoBeganWith() {
        let source = Self.function(
            """
            let process = Process()
            let pipe = Pipe()
            do {
                process.standardOutput = pipe
                try process.run()
                _ = pipe.fileHandleForReading.readDataToEndOfFile()
            } catch {
                process.waitUntilExit()
                throw error
            }
            process.waitUntilExit()
            return Data()
            """
        )
        #expect(Self.sites(source) == ["9:5 pipe"])
    }

    @Test func eachUndrainedPipeIsOneFindingAtTheFirstWait() {
        let source = Self.function(
            """
            let process = Process()
            let outputPipe = Pipe()
            let errorPipe = Pipe()
            process.standardOutput = outputPipe
            process.standardError = errorPipe
            try process.run()
            let errors = errorPipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            process.waitUntilExit()
            return errors
            """
        )
        #expect(Self.sites(source) == ["9:1 outputPipe"])
    }

    @Test func onePipeForBothStreamsIsOneFindingNamingBoth() {
        let source = Self.function(
            """
            let process = Process()
            let pipe = Pipe()
            process.standardOutput = pipe
            process.standardError = pipe
            try process.run()
            process.waitUntilExit()
            return pipe.fileHandleForReading.readDataToEndOfFile()
            """
        )
        let found = Self.findings(source)
        #expect(found.map { "\($0.line):\($0.column)" } == ["7:1"])
        #expect(found.first?.message.contains("writes its standard output and standard error to") == true)
    }

    @Test func aDrainThroughTheHandlesLetCounts() {
        let drained = Self.function(
            Self.launched + """

                let handle = pipe.fileHandleForReading
                let output = handle.readDataToEndOfFile()
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(drained) == [])
        let waitingFirst = Self.function(
            Self.launched + """

                let handle = pipe.fileHandleForReading
                process.waitUntilExit()
                return handle.readDataToEndOfFile()
                """
        )
        #expect(Self.sites(waitingFirst) == ["7:1 pipe"])
    }

    @Test func aStreamGivenSomethingElseIsNoLongerThePipe() {
        let source = Self.function(
            Self.launched + """

                process.standardOutput = FileHandle.nullDevice
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aClosedReadingEndCannotBlockTheChild() {
        let source = Self.function(
            Self.launched + """

                try pipe.fileHandleForReading.close()
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func topLevelCodeIsABody() {
        let source = """
            import Foundation

            let process = Process()
            let pipe = Pipe()
            process.standardOutput = pipe
            try process.run()
            process.waitUntilExit()
            print(String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self))
            """
        #expect(Self.sites(source) == ["7:1 pipe"])
    }

    /* Presence's `Kingdom.read`, on main at 01e3d50e: stdout drained first, stderr a `Pipe()` nothing can read. */
    static let kingdom = """
        private static func read(_ options: [String]) async -> [Position] {
            await withCheckedContinuation { continuation in
                DispatchQueue.global(qos: .userInitiated).async {
                    let process = Process()
                    process.executableURL = URL(fileURLWithPath: "/bin/zsh")
                    process.arguments =
                        ["-lc", #"cd ~/Projects/ahra && ahra os kingdom "$@" 2>/dev/null"#, "zsh"] + options

                    let output = Pipe()
                    process.standardOutput = output
                    process.standardError = Pipe()

                    guard (try? process.run()) != nil else {
                        continuation.resume(returning: [])
                        return
                    }
                    let data = output.fileHandleForReading.readDataToEndOfFile()
                    process.waitUntilExit()

                    let text = String(data: data, encoding: .utf8) ?? ""
                    continuation.resume(returning: Self.parse(text))
                }
            }
        }
        """

    @Test func aPipeGivenStraightToAStreamIsNeverRead() {
        let found = Self.findings(Self.kingdom)
        #expect(found.map { "\($0.line):\($0.column)" } == ["18:13"])
        #expect(found.first?.messageId == "childOutputNeverRead")
        #expect(
            found.first?.message
                == "`process` writes its standard error to a `Pipe()` nothing reads, and some path waits for it at `process.waitUntilExit()`, so output past the pipe buffer blocks the child and the wait never returns. Give the stream `FileHandle.nullDevice` when nothing needs it, or keep the pipe in a `let` and read it to its end before the wait."
        )
    }

    @Test func aPipeGivenStraightToAStreamAndReadBackIsSilent() {
        let readBack = Self.function(
            """
            let process = Process()
            process.standardOutput = Pipe()
            try process.run()
            let output = (process.standardOutput as? Pipe)?.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            return output ?? Data()
            """
        )
        #expect(Self.sites(readBack) == [])
        let replaced = Self.function(
            """
            let process = Process()
            process.standardError = Pipe()
            process.standardError = FileHandle.nullDevice
            try process.run()
            process.waitUntilExit()
            return Data()
            """
        )
        #expect(Self.sites(replaced) == [])
    }

    // MARK: Clean by #pte6ec9

    @Test func aReadabilityHandlerSetBeforeTheWaitIsClean() {
        let source = Self.function(
            Self.launched + """

                var output = Data()
                pipe.fileHandleForReading.readabilityHandler = { handle in output.append(handle.availableData) }
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aReadabilityHandlerClearedIsNoDrain() {
        let source = Self.function(
            Self.launched + """

                pipe.fileHandleForReading.readabilityHandler = nil
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aStreamThatIsNotAPipeIsClean() {
        let discarded = Self.function(
            """
            let process = Process()
            process.standardOutput = FileHandle.nullDevice
            process.standardError = FileHandle.nullDevice
            try process.run()
            process.waitUntilExit()
            return Data()
            """
        )
        #expect(Self.sites(discarded) == [])
        let inherited = Self.function(
            """
            let process = Process()
            let pipe = Pipe()
            process.standardInput = pipe
            try process.run()
            process.waitUntilExit()
            return Data()
            """
        )
        #expect(Self.sites(inherited) == [])
    }

    @Test func aProcessNeverWaitedOnIsClean() {
        let source = Self.function(
            Self.launched + """

                process.terminationHandler = { _ in }
                return Data()
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func drainingStandardErrorLeavesStandardOutputsPipeReported() {
        let source = Self.function(
            """
            let process = Process()
            let outputPipe = Pipe()
            let errorPipe = Pipe()
            process.standardOutput = outputPipe
            process.standardError = errorPipe
            try process.run()
            errorPipe.fileHandleForReading.readabilityHandler = { _ in }
            process.waitUntilExit()
            return outputPipe.fileHandleForReading.readDataToEndOfFile()
            """
        )
        #expect(Self.sites(source) == ["9:1 outputPipe"])
    }

    // MARK: Silence, where the drain or the wait cannot be placed

    @Test func aReadStartedOnAnotherQueueIsSilent() {
        let source = Self.function(
            Self.launched + """

                var output = Data()
                let group = DispatchGroup()
                DispatchQueue.global().async(group: group) {
                    output = pipe.fileHandleForReading.readDataToEndOfFile()
                }
                process.waitUntilExit()
                group.wait()
                return output
                """
        )
        #expect(Self.sites(source) == [])
    }

    @Test func aDrainOrWaitInsideALoopOrDeferIsSilent() {
        let inLoop = Self.function(
            Self.launched + """

                var output = Data()
                while process.isRunning {
                    output.append(pipe.fileHandleForReading.availableData)
                }
                process.waitUntilExit()
                return output
                """
        )
        #expect(Self.sites(inLoop) == [])
        let deferred = Self.function(
            Self.launched + """

                defer { _ = pipe.fileHandleForReading.readDataToEndOfFile() }
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(deferred) == [])
        let waitInLoop = Self.function(
            Self.launched + """

                for _ in 0..<1 {
                    process.waitUntilExit()
                }
                return pipe.fileHandleForReading.readDataToEndOfFile()
                """
        )
        #expect(Self.sites(waitInLoop) == [])
    }

    @Test func aPipeOrProcessHandedAwayIsSilent() {
        let pipeHanded = Self.function(
            Self.launched + """

                collect(pipe)
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(pipeHanded) == [])
        let processHanded = Self.function(
            Self.launched + """

                drainOutput(of: process)
                process.waitUntilExit()
                return Data()
                """
        )
        #expect(Self.sites(processHanded) == [])
        let streamReadBack = Self.function(
            Self.launched + """

                let stream = process.standardOutput as? Pipe
                process.waitUntilExit()
                return stream?.fileHandleForReading.readDataToEndOfFile() ?? Data()
                """
        )
        #expect(Self.sites(streamReadBack) == [])
        let partialRead = Self.function(
            Self.launched + """

                let first = pipe.fileHandleForReading.readData(ofLength: 16)
                process.waitUntilExit()
                return first
                """
        )
        #expect(Self.sites(partialRead) == [])
    }

    @Test func aNameNotCertainIsSilent() {
        let variable = Self.function(
            """
            var process = Process()
            let pipe = Pipe()
            process.standardOutput = pipe
            try process.run()
            process.waitUntilExit()
            return pipe.fileHandleForReading.readDataToEndOfFile()
            """
        )
        #expect(Self.sites(variable) == [])
        let boundTwice = Self.function(
            Self.launched + """

                if verbose {
                    let pipe = Pipe()
                    _ = pipe
                }
                process.waitUntilExit()
                return pipe.fileHandleForReading.readDataToEndOfFile()
                """
        )
        #expect(Self.sites(boundTwice) == [])
        let ownPipeType = """
            struct Pipe {
                let fileHandleForReading = FileHandle.nullDevice
            }

            func run() {
                let process = Process()
                let pipe = Pipe()
                process.standardOutput = pipe
                process.waitUntilExit()
                _ = pipe.fileHandleForReading.readDataToEndOfFile()
            }
            """
        #expect(Self.sites(ownPipeType) == [])
        let memberOfTheSameName = Self.function(
            Self.launched + """

                _ = self.pipe
                process.waitUntilExit()
                return pipe.fileHandleForReading.readDataToEndOfFile()
                """
        )
        #expect(Self.sites(memberOfTheSameName) == ["7:1 pipe"])
    }

    @Test func aJumpTheWalkDoesNotLayOutSilencesTheBody() {
        let source = Self.function(
            Self.launched + """

                switch mode {
                    case 0:
                        fallthrough
                    default:
                        break
                }
                process.waitUntilExit()
                return pipe.fileHandleForReading.readDataToEndOfFile()
                """
        )
        #expect(Self.sites(source) == [])
    }
}
