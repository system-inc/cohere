import SwiftOperators
import SwiftSyntax

/*
 No waiting for a child to exit while its output sits in a pipe nobody reads:

     let process = Process()
     let pipe = Pipe()
     process.standardOutput = pipe
     try process.run()
     process.waitUntilExit()
     let data = pipe.fileHandleForReading.readDataToEndOfFile()

 A pipe holds 64 KB. A child that writes more blocks in `write` until someone reads, and the parent is in
 `waitUntilExit()` waiting for it to exit first, so neither moves. Servers.app's CPU sampler ran `ps -eo
 pid,ppid,%cpu` this way every three seconds; once the process table outgrew the pipe, each tick parked one more
 `ps` child and one more GCD thread, until 80 hung children helped run the Mac out of process slots on 2026-11-10
 (servers-macos 5c5833e, whose `HelperCommand` reads to end of file first and then waits). The repair drains the
 pipe before the wait, or hands it to something that drains it as the child writes.

 It ports `nexus/correctness-require-drained-child-output`, the shared judgment #pte6ec9 sets for every engine:
 - The child and its pipes. A process is a local `let` initialized from `Process()` (or `Foundation.Process()`),
   and a pipe a local `let` from `Pipe()`, each name bound once in the body, so a name is certain to mean that
   value wherever it is read; a file that declares its own type named `Process` or `Pipe` is not read at all. A
   pipe is the process's when `process.standardOutput = pipe` or `process.standardError = pipe` assigns it, and
   stops being when the same stream is given anything else (`FileHandle.nullDevice`, a file). A stream left
   unset is inherited, and a stream that is not a `Pipe()` is not a pipe this rule knows, so neither is read.
 - A `Pipe()` given straight to the stream (`process.standardError = Pipe()`) is a pipe too, and a certain one
   nothing drains: no name can reach its reading end, and reading the stream back silences the process (below).
   Its finding says so in its own words, since there is no `let` to read it through. This is the shape of the
   stderr pipe TypeScript's census found in ahra's PresenceApi.ts, which `stdio` left a pipe nobody read.
 - The wait is `process.waitUntilExit()`. It is reported when some path reaches it with the process's pipe
   still undrained: one finding per pipe, at the first such wait, naming the pipe, the process and the stream
   (both streams when one pipe takes them both).
 - A drain is the pipe's `fileHandleForReading` read to its end (`readDataToEndOfFile()`, `readToEnd()`), handed
   to a concurrent drain (a `readabilityHandler` set to anything but `nil`), or closed (`close()`, `closeFile()`),
   which leaves the child's writes nowhere to block: it gets `SIGPIPE` or `EPIPE` and the wait returns. The
   handle may be read through a `let` bound to it once (`let handle = pipe.fileHandleForReading`).

 Paths are decided on the syntax tree of one body (a function, initializer, accessor, closure or the file's
 top-level code), since this engine has no control-flow graph. The walk carries, for each path, the pipes that path
 attached to a process's stream and has not drained since, and asks at each wait. An attach adds its pipe (and
 takes away whatever the stream held before), a drain takes it away, and where paths join the set is the union of
 theirs, so a pipe is reported only when some path reaches the wait with it attached and undrained. Sequential
 statements run in source order. `if` and `else`, `guard`, `switch` and `do` with its `catch` clauses join their
 branches: a drain on one branch of an `if` after an attach before it is red, a drain on both, or before the
 branching, is green, and a branch that attaches and drains beside one that never attaches is green. `return`,
 `throw`, `fatalError`, `preconditionFailure`, `exit` and `abort` end their path, and a `guard`'s `else` never falls
 through. A condition list's first element runs on both branches and the rest only on the one that passed; a
 `catch` clause starts from what was undrained when the `do` began, with every pipe the body attached. Within one
 expression the order is the source's, so `&&`, `??`, the ternary and an `if` used as a value count a drain that
 may not run, which only silences.

 What falls to silence, never reported, because the drain or the wait cannot be placed with certainty:
 - The pipe named inside a closure, a nested function, a `defer`, a loop or a `#if`, where it may run before the
   wait or after it, or many times: so a read started on another queue or task before the wait, which is the
   concurrent drain #pte6ec9 calls clean, is silent with everything else there. A wait inside one of them is
   not read, and the stream assigned inside one silences the process.
 - The pipe used any other way: handed to a function, stored, returned, given to another process as its input,
   or its reading handle read in part (`availableData`, `readData(ofLength:)`, `bytes`). The receiver may drain
   it.
 - The process used any way but its plain members (`executableURL`, `arguments`, `run()`, `isRunning`,
   `processIdentifier`, `terminationStatus`, a `terminationHandler` and the like): handed to a function, stored,
   returned, or its stream read back (`process.standardOutput as? Pipe`). The receiver may drain the pipe.
 - A body with a labeled `break`, a `continue` or `fallthrough` this walk would reach, or a `switch` whose cases
   hold a `#if`: their jumps are not laid out, so the whole body is silent.

 Known misses, every one a finding not made and never one invented:
 - A process or pipe bound any other way: `var`, a stored property, a parameter, a helper that returns one,
   `Process.run(_:arguments:)`.
 - Every silence above. The engine has no channel to count a site it declined, so the census counts them by
   reading the consumers.
 - A stream assigned on one path and drained on another whose conditions agree (`if verbose` twice) is red, as a
   one-branch drain is: the walk does not correlate conditions, and on the path where they disagree the deadlock
   is real.

 No fix: whether to read the output, drain it as it comes, or discard it is the author's call.
 */
public struct CorrectnessRequireDrainedChildOutput: FileRule {
    public static let ruleName = "cohere-swift/correctness-require-drained-child-output"
    public let name = Self.ruleName
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    public init() {}

    /* Every finding is a `waitUntilExit()` on a process whose stream is a `Pipe()` bound in the same body. */
    public func applies(to file: ParsedFile) -> Bool {
        file.source.contains("waitUntilExit") && file.source.contains("Pipe(")
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        /* Folded, so `process.standardOutput = pipe` is one assignment. Folding moves no token. */
        let folded = OperatorTable.standardOperators.foldAll(file.tree) { _ in }
        let finder = Finder(viewMode: .sourceAccurate)
        finder.walk(folded)
        guard !finder.declaresFoundationName else { return [] }
        return
            finder.roots
            .flatMap { Analysis(root: $0).reports() }
            .sorted { $0.wait.positionAfterSkippingLeadingTrivia < $1.wait.positionAfterSkippingLeadingTrivia }
            .map { report in
                let stream = report.streams.map(\.spoken).joined(separator: " and ")
                return file.finding(
                    at: report.wait,
                    rule: name,
                    message: report.pipe.hasPrefix(Self.unnamedPipe)
                        ? RuleMessages.CorrectnessRequireDrainedChildOutput.childOutputNeverRead(
                            process: report.process,
                            stream: stream,
                        )
                        : RuleMessages.CorrectnessRequireDrainedChildOutput.childOutputNeverDrained(
                            pipe: report.pipe,
                            process: report.process,
                            stream: stream,
                        ),
                )
            }
    }

    /*
     How a `Pipe()` given straight to a stream (`process.standardError = Pipe()`) is known, followed by where it is
     written. No name can start this way, so it never meets a `let`. Nothing can read such a pipe but the stream read
     back, which silences the process, so it is never drained.
     */
    static let unnamedPipe = "Pipe()@"

    /* The two streams a pipe can take, in the order a finding names them. */
    enum Stream: String, Comparable {
        case standardOutput
        case standardError

        var spoken: String {
            switch self {
                case .standardOutput: "standard output"
                case .standardError: "standard error"
            }
        }

        static func < (left: Stream, right: Stream) -> Bool {
            left == .standardOutput && right == .standardError
        }
    }

    /* One finding before it is placed: the wait, the pipe it waits past, and the streams that pipe takes there. */
    struct Report {
        let wait: FunctionCallExprSyntax
        let pipe: String
        let process: String
        let streams: [Stream]
    }

    /* A body of code walked on its own, and the declaration or closure that owns it. */
    struct Root {
        let items: CodeBlockItemListSyntax
        let owner: Syntax
    }

    /* Every root, and whether the file declares a type that would make `Process()` or `Pipe()` its own. */
    final class Finder: SyntaxVisitor {
        private(set) var roots: [Root] = []
        private(set) var declaresFoundationName = false

        static let foundationNames: Set<String> = ["Process", "Pipe"]

        override func visit(_ node: SourceFileSyntax) -> SyntaxVisitorContinueKind {
            roots.append(Root(items: node.statements, owner: Syntax(node)))
            return .visitChildren
        }

        override func visit(_ node: FunctionDeclSyntax) -> SyntaxVisitorContinueKind {
            if let body = node.body {
                roots.append(Root(items: body.statements, owner: Syntax(node)))
            }
            return .visitChildren
        }

        override func visit(_ node: InitializerDeclSyntax) -> SyntaxVisitorContinueKind {
            if let body = node.body {
                roots.append(Root(items: body.statements, owner: Syntax(node)))
            }
            return .visitChildren
        }

        override func visit(_ node: DeinitializerDeclSyntax) -> SyntaxVisitorContinueKind {
            if let body = node.body {
                roots.append(Root(items: body.statements, owner: Syntax(node)))
            }
            return .visitChildren
        }

        override func visit(_ node: AccessorDeclSyntax) -> SyntaxVisitorContinueKind {
            if let body = node.body {
                roots.append(Root(items: body.statements, owner: Syntax(node)))
            }
            return .visitChildren
        }

        override func visit(_ node: AccessorBlockSyntax) -> SyntaxVisitorContinueKind {
            if case .getter(let items) = node.accessors {
                roots.append(Root(items: items, owner: Syntax(node)))
            }
            return .visitChildren
        }

        override func visit(_ node: ClosureExprSyntax) -> SyntaxVisitorContinueKind {
            roots.append(Root(items: node.statements, owner: Syntax(node)))
            return .visitChildren
        }

        override func visit(_ node: ClassDeclSyntax) -> SyntaxVisitorContinueKind {
            declare(node.name)
            return .visitChildren
        }

        override func visit(_ node: StructDeclSyntax) -> SyntaxVisitorContinueKind {
            declare(node.name)
            return .visitChildren
        }

        override func visit(_ node: EnumDeclSyntax) -> SyntaxVisitorContinueKind {
            declare(node.name)
            return .visitChildren
        }

        override func visit(_ node: ActorDeclSyntax) -> SyntaxVisitorContinueKind {
            declare(node.name)
            return .visitChildren
        }

        override func visit(_ node: TypeAliasDeclSyntax) -> SyntaxVisitorContinueKind {
            declare(node.name)
            return .visitChildren
        }

        func declare(_ name: TokenSyntax) {
            if Self.foundationNames.contains(CorrectnessRequireDrainedChildOutput.plainName(name)) {
                declaresFoundationName = true
            }
        }
    }

    /* A name as it is meant, without the backticks that let a keyword be one. */
    static func plainName(_ token: TokenSyntax) -> String {
        String(token.text.filter { $0 != "`" })
    }

    /* An expression without the parentheses, `try` and `await` around it, which do not change which value it is. */
    static func stripped(_ expression: ExprSyntax) -> ExprSyntax {
        var current = expression
        while true {
            if let wrapper = current.as(TryExprSyntax.self) {
                current = wrapper.expression
            }
            else if let wrapper = current.as(AwaitExprSyntax.self) {
                current = wrapper.expression
            }
            else if let tuple = current.as(TupleExprSyntax.self), tuple.elements.count == 1,
                let only = tuple.elements.first, only.label == nil
            {
                current = only.expression
            }
            else {
                return current
            }
        }
    }

    /* The assignment a node is the left side of: `process.standardOutput = pipe`, `handle.readabilityHandler = ...`. */
    static func assignment(assigning node: Syntax) -> InfixOperatorExprSyntax? {
        guard let assignment = node.parent?.as(InfixOperatorExprSyntax.self),
            assignment.operator.is(AssignmentExprSyntax.self), Syntax(assignment.leftOperand).id == node.id
        else { return nil }
        return assignment
    }

    /* The one node a reference's value is used as, out through the parentheses around it. */
    static func outward(_ node: Syntax) -> Syntax {
        var current = node
        while let element = current.parent?.as(LabeledExprSyntax.self), element.label == nil,
            let list = element.parent?.as(LabeledExprListSyntax.self), list.count == 1,
            let tuple = list.parent?.as(TupleExprSyntax.self)
        {
            current = Syntax(tuple)
        }
        return current
    }

    /*
     One body's walk: which names are a process, a pipe or a pipe's reading handle, what each reference to them
     does, and then the paths through the body, asking at every wait which of the process's pipes some path left
     undrained.
     */
    final class Analysis {
        enum Event {
            /* The stream given this pipe, or, with no pipe, given something else. */
            case attach(process: String, stream: Stream, pipe: String?)
            case drain(pipe: String)
            case wait(process: String, call: FunctionCallExprSyntax)
        }

        /* A pipe one process writes a stream to. */
        struct Attachment: Hashable {
            let process: String
            let stream: Stream
            let pipe: String
        }

        /* What is known at one point of a path: whether it runs, and which pipes it left attached and undrained. */
        struct State {
            var reachable = true
            var undrained: Set<Attachment> = []

            static let unreachable = State(reachable: false)
        }

        /* The members of a process that hand none of its streams anywhere. */
        static let plainProcessMembers: Set<String> = [
            "executableURL", "launchPath", "arguments", "environment", "currentDirectoryURL", "currentDirectoryPath",
            "standardInput", "run", "launch", "terminate", "interrupt", "suspend", "resume", "isRunning",
            "processIdentifier", "terminationStatus", "terminationReason", "terminationHandler", "qualityOfService",
        ]

        /* The calls on a pipe's reading handle after which the child cannot block on it. */
        static let drainingCalls: Set<String> = ["readDataToEndOfFile", "readToEnd", "close", "closeFile"]

        let root: Root
        var processes: Set<String> = []
        var pipes: Set<String> = []
        /* A `let` bound to a pipe's `fileHandleForReading`, by its name, to the pipe's. */
        var handles: [String: String] = [:]
        var dropped: Set<String> = []
        var escaped: Set<String> = []
        var events: [(position: AbsolutePosition, event: Event)] = []
        var found: [Report] = []
        /* The states an unlabeled `break` carries out of each `switch` being walked, innermost last. */
        var breakStates: [[State]] = []
        var aborted = false

        init(root: Root) {
            self.root = root
        }

        func reports() -> [Report] {
            collectNames()
            guard !processes.isEmpty else { return [] }
            classifyReferences()
            _ = walkItems(root.items, State())
            guard !aborted else { return [] }

            var firstByPipe: [String: Report] = [:]
            for report in found where !dropped.contains(report.pipe) && !escaped.contains(report.process) {
                guard let earlier = firstByPipe[report.pipe] else {
                    firstByPipe[report.pipe] = report
                    continue
                }
                let position = report.wait.positionAfterSkippingLeadingTrivia
                let earlierPosition = earlier.wait.positionAfterSkippingLeadingTrivia
                if position < earlierPosition {
                    firstByPipe[report.pipe] = report
                }
                else if position == earlierPosition {
                    firstByPipe[report.pipe] = Report(
                        wait: earlier.wait,
                        pipe: earlier.pipe,
                        process: earlier.process,
                        streams: Array(Set(earlier.streams + report.streams)).sorted(),
                    )
                }
            }
            return Array(firstByPipe.values)
        }

        // MARK: Names

        /*
         The processes and pipes this body declares, and the handles bound to their reading ends, kept only when
         their name is bound once anywhere the body reaches, nested closures and its own parameters included.
         */
        func collectNames() {
            let declarations = Declarations(root: root)
            declarations.walk(root.items)
            let bindings = Bindings(viewMode: .sourceAccurate)
            bindings.walk(root.owner)
            let once = Set(bindings.counts.filter { $0.value == 1 }.keys)
            processes = declarations.processes.intersection(once)
            pipes = declarations.pipes.intersection(once)
            for (name, pipe) in declarations.handles where once.contains(name) && pipes.contains(pipe) {
                handles[name] = pipe
            }
        }

        /* The `let` declarations of the body itself, never of a closure or function nested in it. */
        final class Declarations: SyntaxVisitor {
            let root: Root
            private(set) var processes: Set<String> = []
            private(set) var pipes: Set<String> = []
            private(set) var handles: [String: String] = [:]

            init(root: Root) {
                self.root = root
                super.init(viewMode: .sourceAccurate)
            }

            /* A closure, a nested declaration and a computed variable's accessors are bodies of their own. */
            override func visit(_ node: ClosureExprSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: FunctionDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: AccessorBlockSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: ClassDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: StructDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: EnumDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: ActorDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: ExtensionDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: ProtocolDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: InitializerDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: DeinitializerDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: SubscriptDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }
            override func visit(_ node: IfConfigDeclSyntax) -> SyntaxVisitorContinueKind { .skipChildren }

            override func visit(_ node: VariableDeclSyntax) -> SyntaxVisitorContinueKind {
                guard node.bindingSpecifier.tokenKind == .keyword(.let),
                    !node.modifiers.contains(where: { $0.name.text == "lazy" || $0.name.text == "async" })
                else { return .visitChildren }
                for binding in node.bindings {
                    guard let identifier = binding.pattern.as(IdentifierPatternSyntax.self),
                        let value = binding.initializer?.value
                    else { continue }
                    let name = CorrectnessRequireDrainedChildOutput.plainName(identifier.identifier)
                    let annotation = binding.typeAnnotation?.type.as(IdentifierTypeSyntax.self)?.name.text
                    if Self.isInitializer(value, of: "Process"), [nil, "Process"].contains(annotation) {
                        processes.insert(name)
                    }
                    else if Self.isInitializer(value, of: "Pipe"), [nil, "Pipe"].contains(annotation) {
                        pipes.insert(name)
                    }
                    else if binding.typeAnnotation == nil,
                        let access = value.as(MemberAccessExprSyntax.self),
                        access.declName.baseName.text == "fileHandleForReading", access.declName.argumentNames == nil,
                        let base = access.base?.as(DeclReferenceExprSyntax.self), base.argumentNames == nil
                    {
                        handles[name] = CorrectnessRequireDrainedChildOutput.plainName(base.baseName)
                    }
                }
                return .visitChildren
            }

            /* `Process()` or `Foundation.Process()`, with nothing passed. */
            static func isInitializer(_ value: ExprSyntax, of type: String) -> Bool {
                guard let call = value.as(FunctionCallExprSyntax.self), call.arguments.isEmpty,
                    call.trailingClosure == nil, call.additionalTrailingClosures.isEmpty
                else { return false }
                if let reference = call.calledExpression.as(DeclReferenceExprSyntax.self) {
                    return reference.baseName.text == type && reference.argumentNames == nil
                }
                if let member = call.calledExpression.as(MemberAccessExprSyntax.self),
                    member.base?.as(DeclReferenceExprSyntax.self)?.baseName.text == "Foundation"
                {
                    return member.declName.baseName.text == type && member.declName.argumentNames == nil
                }
                return false
            }
        }

        /* How many times each name is bound: by a pattern, a closure's parameter or a function's. */
        final class Bindings: SyntaxVisitor {
            private(set) var counts: [String: Int] = [:]

            func count(_ token: TokenSyntax) {
                counts[CorrectnessRequireDrainedChildOutput.plainName(token), default: 0] += 1
            }

            override func visit(_ node: IdentifierPatternSyntax) -> SyntaxVisitorContinueKind {
                count(node.identifier)
                return .visitChildren
            }

            override func visit(_ node: ClosureShorthandParameterSyntax) -> SyntaxVisitorContinueKind {
                count(node.name)
                return .visitChildren
            }

            override func visit(_ node: ClosureParameterSyntax) -> SyntaxVisitorContinueKind {
                count(node.secondName ?? node.firstName)
                return .visitChildren
            }

            override func visit(_ node: FunctionParameterSyntax) -> SyntaxVisitorContinueKind {
                count(node.secondName ?? node.firstName)
                return .visitChildren
            }
        }

        // MARK: References

        /* Every reference to a tracked name, read as an event on the path, a plain use, or a use that silences it. */
        func classifyReferences() {
            let references = References(viewMode: .sourceAccurate)
            references.walk(root.owner)
            for reference in references.found where reference.argumentNames == nil {
                let name = CorrectnessRequireDrainedChildOutput.plainName(reference.baseName)
                let placed = isPlaced(Syntax(reference))
                if processes.contains(name) {
                    classifyProcess(reference, name: name, placed: placed)
                }
                else if pipes.contains(name) {
                    classifyPipe(reference, name: name, placed: placed)
                }
                else if let pipe = handles[name] {
                    let event = handleEvent(Syntax(reference), pipe: pipe)
                    record(event, at: reference, placed: placed, pipe: pipe)
                }
            }
            events.sort { $0.position < $1.position }
        }

        /* Every name read as a value. A member's name (`self.pipe`, `\.pipe`) is some other declaration's. */
        final class References: SyntaxVisitor {
            private(set) var found: [DeclReferenceExprSyntax] = []

            override func visit(_ node: DeclReferenceExprSyntax) -> SyntaxVisitorContinueKind {
                if let access = node.parent?.as(MemberAccessExprSyntax.self), access.declName.id == node.id {
                    return .visitChildren
                }
                if node.parent?.is(KeyPathPropertyComponentSyntax.self) == true {
                    return .visitChildren
                }
                found.append(node)
                return .visitChildren
            }
        }

        /*
         Whether a node runs where this body's walk can place it: inside the body's own statements, and not inside a
         closure, a nested declaration, a `defer`, a loop, a `#if`, or an initializer that runs later.
         */
        func isPlaced(_ node: Syntax) -> Bool {
            var current = node.parent
            while let candidate = current {
                if candidate.id == root.items.id {
                    return true
                }
                if candidate.is(ClosureExprSyntax.self) || candidate.is(DeferStmtSyntax.self)
                    || candidate.is(WhileStmtSyntax.self) || candidate.is(RepeatStmtSyntax.self)
                    || candidate.is(ForStmtSyntax.self) || candidate.is(IfConfigDeclSyntax.self)
                    || candidate.is(AccessorBlockSyntax.self)
                {
                    return false
                }
                if let declaration = candidate.as(VariableDeclSyntax.self) {
                    if declaration.modifiers.contains(where: { $0.name.text == "lazy" || $0.name.text == "async" }) {
                        return false
                    }
                }
                else if candidate.is(DeclSyntax.self) {
                    return false
                }
                current = candidate.parent
            }
            return false
        }

        func classifyProcess(_ reference: DeclReferenceExprSyntax, name: String, placed: Bool) {
            guard let access = reference.parent?.as(MemberAccessExprSyntax.self),
                access.base.map({ Syntax($0).id == Syntax(reference).id }) == true,
                access.declName.argumentNames == nil
            else {
                escaped.insert(name)
                return
            }
            let member = CorrectnessRequireDrainedChildOutput.plainName(access.declName.baseName)
            if member == "waitUntilExit", let call = access.parent?.as(FunctionCallExprSyntax.self),
                Syntax(call.calledExpression).id == Syntax(access).id, call.arguments.isEmpty,
                call.trailingClosure == nil
            {
                /* A wait the walk cannot place is not read: it neither reports nor silences. */
                if placed {
                    events.append((reference.positionAfterSkippingLeadingTrivia, .wait(process: name, call: call)))
                }
                return
            }
            if let stream = Stream(rawValue: member) {
                guard placed,
                    let assignment = CorrectnessRequireDrainedChildOutput.assignment(assigning: Syntax(access))
                else {
                    escaped.insert(name)
                    return
                }
                var pipe: String?
                let value = CorrectnessRequireDrainedChildOutput.stripped(assignment.rightOperand)
                if let given = value.as(DeclReferenceExprSyntax.self), given.argumentNames == nil {
                    let givenName = CorrectnessRequireDrainedChildOutput.plainName(given.baseName)
                    pipe = pipes.contains(givenName) ? givenName : nil
                }
                else if Declarations.isInitializer(value, of: "Pipe") {
                    pipe = CorrectnessRequireDrainedChildOutput.unnamedPipe + "\(value.position.utf8Offset)"
                }
                events.append(
                    (reference.positionAfterSkippingLeadingTrivia, .attach(process: name, stream: stream, pipe: pipe))
                )
                return
            }
            if !Self.plainProcessMembers.contains(member) {
                escaped.insert(name)
            }
        }

        func classifyPipe(_ reference: DeclReferenceExprSyntax, name: String, placed: Bool) {
            let node = CorrectnessRequireDrainedChildOutput.outward(Syntax(reference))
            /* The value of `process.standardOutput = pipe`: the attach the process's reference records. */
            if let assignment = node.parent?.as(InfixOperatorExprSyntax.self),
                assignment.operator.is(AssignmentExprSyntax.self),
                CorrectnessRequireDrainedChildOutput.stripped(assignment.rightOperand).id == ExprSyntax(reference).id,
                let target = assignment.leftOperand.as(MemberAccessExprSyntax.self),
                Stream(rawValue: target.declName.baseName.text) != nil,
                let process = target.base?.as(DeclReferenceExprSyntax.self),
                processes.contains(CorrectnessRequireDrainedChildOutput.plainName(process.baseName))
            {
                if !placed {
                    dropped.insert(name)
                }
                return
            }
            guard let access = node.parent?.as(MemberAccessExprSyntax.self),
                access.base.map({ Syntax($0).id == node.id }) == true, access.declName.argumentNames == nil
            else {
                dropped.insert(name)
                return
            }
            switch CorrectnessRequireDrainedChildOutput.plainName(access.declName.baseName) {
                case "fileHandleForWriting":
                    break
                case "fileHandleForReading":
                    record(handleEvent(Syntax(access), pipe: name), at: reference, placed: placed, pipe: name)
                default:
                    dropped.insert(name)
            }
        }

        enum HandleUse {
            case drain
            case alias
            case unknown
        }

        /* What is done with a pipe's reading handle, written out (`pipe.fileHandleForReading`) or through its `let`. */
        func handleEvent(_ handle: Syntax, pipe: String) -> HandleUse {
            if let access = handle.parent?.as(MemberAccessExprSyntax.self),
                access.base.map({ Syntax($0).id == handle.id }) == true, access.declName.argumentNames == nil
            {
                let member = CorrectnessRequireDrainedChildOutput.plainName(access.declName.baseName)
                if Self.drainingCalls.contains(member), let call = access.parent?.as(FunctionCallExprSyntax.self),
                    Syntax(call.calledExpression).id == Syntax(access).id, call.arguments.isEmpty,
                    call.trailingClosure == nil
                {
                    return .drain
                }
                if member == "readabilityHandler",
                    let assignment = CorrectnessRequireDrainedChildOutput.assignment(assigning: Syntax(access)),
                    !CorrectnessRequireDrainedChildOutput.stripped(assignment.rightOperand).is(
                        NilLiteralExprSyntax.self
                    )
                {
                    return .drain
                }
                return .unknown
            }
            if let binding = handle.parent?.as(InitializerClauseSyntax.self)?.parent?.as(PatternBindingSyntax.self),
                let identifier = binding.pattern.as(IdentifierPatternSyntax.self),
                handles[CorrectnessRequireDrainedChildOutput.plainName(identifier.identifier)] == pipe
            {
                return .alias
            }
            return .unknown
        }

        /* A use of a pipe's handle: a drain the walk can place is an event, and anything else silences the pipe. */
        func record(_ use: HandleUse, at reference: DeclReferenceExprSyntax, placed: Bool, pipe: String) {
            switch use {
                case .drain:
                    if placed {
                        events.append((reference.positionAfterSkippingLeadingTrivia, .drain(pipe: pipe)))
                    }
                    else {
                        dropped.insert(pipe)
                    }
                case .alias:
                    if !placed {
                        dropped.insert(pipe)
                    }
                case .unknown:
                    dropped.insert(pipe)
            }
        }

        // MARK: Paths

        static func join(_ states: [State]) -> State {
            let live = states.filter(\.reachable)
            guard var joined = live.first else { return .unreachable }
            for state in live.dropFirst() {
                joined.undrained.formUnion(state.undrained)
            }
            return joined
        }

        /* Applies every event inside a node, in source order. */
        func apply(_ node: Syntax, _ entry: State) -> State {
            var state = entry
            let range = node.position..<node.endPosition
            for (position, event) in events where range.contains(position) {
                guard state.reachable else { break }
                switch event {
                    case .attach(let process, let stream, let pipe):
                        state.undrained = state.undrained.filter { $0.process != process || $0.stream != stream }
                        if let pipe {
                            state.undrained.insert(Attachment(process: process, stream: stream, pipe: pipe))
                        }
                    case .drain(let pipe):
                        state.undrained = state.undrained.filter { $0.pipe != pipe }
                    case .wait(let process, let call):
                        for attachment in state.undrained where attachment.process == process {
                            let report = Report(
                                wait: call,
                                pipe: attachment.pipe,
                                process: process,
                                streams: [attachment.stream],
                            )
                            found.append(report)
                        }
                }
            }
            return state
        }

        func walkItems(_ items: CodeBlockItemListSyntax, _ entry: State) -> State {
            var state = entry
            for element in items {
                guard state.reachable, !aborted else { return state }
                switch element.item {
                    case .decl(let declaration):
                        /* A nested function, type or `#if` holds nothing placed; a variable's initializer runs here. */
                        if declaration.is(VariableDeclSyntax.self) {
                            state = apply(Syntax(declaration), state)
                        }
                    case .stmt(let statement):
                        state = walkStatement(statement, state)
                    case .expr(let expression):
                        state = walkExpression(expression, state)
                }
            }
            return state
        }

        func walkStatement(_ statement: StmtSyntax, _ state: State) -> State {
            if let expression = statement.as(ExpressionStmtSyntax.self) {
                return walkExpression(expression.expression, state)
            }
            if let labeled = statement.as(LabeledStmtSyntax.self) {
                return walkStatement(labeled.statement, state)
            }
            if statement.is(ReturnStmtSyntax.self) || statement.is(ThrowStmtSyntax.self) {
                _ = apply(Syntax(statement), state)
                return .unreachable
            }
            if let breakStatement = statement.as(BreakStmtSyntax.self) {
                guard breakStatement.label == nil, !breakStates.isEmpty else {
                    aborted = true
                    return .unreachable
                }
                breakStates[breakStates.count - 1].append(state)
                return .unreachable
            }
            if statement.is(ContinueStmtSyntax.self) || statement.is(FallThroughStmtSyntax.self) {
                aborted = true
                return .unreachable
            }
            if let guardStatement = statement.as(GuardStmtSyntax.self) {
                let branches = walkConditions(guardStatement.conditions, state)
                /* The compiler holds `else` to leaving the scope, so its end leads nowhere. */
                _ = walkItems(guardStatement.body.statements, branches.failed)
                return branches.passed
            }
            if let doStatement = statement.as(DoStmtSyntax.self) {
                return walkDo(doStatement, state)
            }
            if statement.is(WhileStmtSyntax.self) || statement.is(RepeatStmtSyntax.self)
                || statement.is(ForStmtSyntax.self) || statement.is(DeferStmtSyntax.self)
            {
                /* Nothing inside is placed: every tracked name it reads was silenced when it was classified. */
                return state
            }
            return apply(Syntax(statement), state)
        }

        func walkExpression(_ expression: ExprSyntax, _ state: State) -> State {
            if let conditional = expression.as(IfExprSyntax.self) {
                return walkIf(conditional, state)
            }
            if let switchExpression = expression.as(SwitchExprSyntax.self) {
                return walkSwitch(switchExpression, state)
            }
            let next = apply(Syntax(expression), state)
            if let call = CorrectnessRequireDrainedChildOutput.stripped(expression).as(FunctionCallExprSyntax.self),
                let callee = call.calledExpression.as(DeclReferenceExprSyntax.self),
                ["fatalError", "preconditionFailure", "exit", "abort"].contains(callee.baseName.text)
            {
                return .unreachable
            }
            return next
        }

        /*
         A condition list's two exits. The first element runs on both; the rest run only on the way to `passed`, so
         `failed` holds what was undrained after the first, and after the last, which any element between may fail to.
         */
        func walkConditions(
            _ conditions: ConditionElementListSyntax,
            _ entry: State,
        ) -> (passed: State, failed: State) {
            var state = entry
            var failed = entry
            for (index, element) in conditions.enumerated() {
                state = apply(Syntax(element), state)
                if index == 0 {
                    failed = state
                }
            }
            failed.undrained.formUnion(state.undrained)
            return (state, failed)
        }

        func walkIf(_ conditional: IfExprSyntax, _ state: State) -> State {
            let branches = walkConditions(conditional.conditions, state)
            let thenEnd = walkItems(conditional.body.statements, branches.passed)
            let elseEnd: State
            switch conditional.elseBody {
                case .ifExpr(let nested):
                    elseEnd = walkIf(nested, branches.failed)
                case .codeBlock(let block):
                    elseEnd = walkItems(block.statements, branches.failed)
                case nil:
                    elseEnd = branches.failed
            }
            return Self.join([thenEnd, elseEnd])
        }

        func walkSwitch(_ switchExpression: SwitchExprSyntax, _ state: State) -> State {
            let subject = apply(Syntax(switchExpression.subject), state)
            breakStates.append([])
            var ends: [State] = []
            for element in switchExpression.cases {
                guard case .switchCase(let switchCase) = element else {
                    /* A `#if` among the cases: which cases exist is the build's decision, not the source's. */
                    aborted = true
                    break
                }
                ends.append(walkItems(switchCase.statements, apply(Syntax(switchCase.label), subject)))
            }
            ends += breakStates.removeLast()
            return Self.join(ends)
        }

        /*
         A `do` with `catch` clauses. A throw may leave the body anywhere, so each clause starts from what was undrained
         when the `do` began, with every pipe the body attached, whatever it drained after.
         */
        func walkDo(_ doStatement: DoStmtSyntax, _ state: State) -> State {
            let bodyEnd = walkItems(doStatement.body.statements, state)
            guard !doStatement.catchClauses.isEmpty else { return bodyEnd }
            var caught = state
            caught.undrained.formUnion(bodyEnd.undrained)
            let body = doStatement.body.position..<doStatement.body.endPosition
            for (position, event) in events where body.contains(position) {
                if case .attach(let process, let stream, let pipe?) = event {
                    caught.undrained.insert(Attachment(process: process, stream: stream, pipe: pipe))
                }
            }
            var ends = [bodyEnd]
            for clause in doStatement.catchClauses {
                ends.append(walkItems(clause.body.statements, apply(Syntax(clause.catchItems), caught)))
            }
            return Self.join(ends)
        }
    }
}
