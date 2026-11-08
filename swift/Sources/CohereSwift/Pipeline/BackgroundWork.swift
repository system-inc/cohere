import Foundation
import Synchronization

/*
 Work started on a queue of its own, which always gets a thread, as the types phase's early check does, and whose
 answer a later step waits for. The run computes side by side and still writes its records in one order: whoever
 writes waits here first. A queue rather than a task, since the work blocks on sourcekitd and child processes,
 which a task would hold a cooperative thread for.
 */
final class BackgroundWork<Value: Sendable>: Sendable {
    private let group = DispatchGroup()
    private let outcome = Mutex<Result<Value, any Error>?>(nil)

    init(label: String, _ work: @escaping @Sendable () throws -> Value) {
        /* Held by the work until it keeps its outcome, so the outcome outlives a caller that never waits. */
        DispatchQueue(label: label).async(group: group) { [self] in
            let finished = Result { try work() }
            outcome.withLock { $0 = finished }
        }
    }

    /* The work's answer, or what it threw, once it is done. */
    func value() throws -> Value {
        group.wait()
        guard let finished = outcome.withLock({ $0 }) else {
            /* The group is left only after the work kept its outcome, so this is never reached. */
            throw Unfinished()
        }
        return try finished.get()
    }

    /* Waits for the work without reading its answer, so nothing it does outlives the step that started it. */
    func wait() {
        group.wait()
    }

    struct Unfinished: Error, CustomStringConvertible {
        var description: String { "background work ended without an outcome" }
    }
}
