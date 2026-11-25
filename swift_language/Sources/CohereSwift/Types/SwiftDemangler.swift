import Foundation
import Synchronization

/*
 The toolchain's own demangler, `libswiftDemangle.dylib` beside `libIndexStore.dylib`: it turns a Swift
 declaration's mangled name into the declaration it spells, signature and all. `$sSTsE7forEachyyy7ElementQzKXEKF`
 is `(extension in Swift):Swift.Sequence.forEach((A.Element) throws -> ()) throws -> ()`.

 The index names the declaration a name resolves to by its unified symbol name, which for a Swift declaration
 is its mangled name with `s:` in place of `$s`. A typed rule that needs more than which declaration (the type
 of a parameter, say) reads it here, in the compiler's own words, rather than in a reading of the mangling of
 ours. Loaded at run time, as the index store library is, so the binary never carries one Xcode's path.

 `@safe` though it holds two C functions: they are private, and their one call is in `call`, which hands each a
 buffer `call` sized and owns.
 */
@safe final class SwiftDemangler: Sendable {
    /* The library could not be found or opened. */
    struct Failure: Error, CustomStringConvertible {
        var description: String
    }

    /* `swift_demangle_getDemangledName(const char *, char *, size_t) -> size_t`: the full length, written up to the buffer's size. */
    private typealias GetDemangledName =
        @convention(c) (UnsafePointer<CChar>?, UnsafeMutablePointer<CChar>?, Int) -> Int

    private let getDemangledName: GetDemangledName
    /* `swift_demangle_getModuleName(const char *, char *, size_t) -> size_t`, the same shape. */
    private let getModuleName: GetDemangledName
    /* Each symbol's module, asked once: a typed rule asks of the same declaration at every place a file names it. */
    private let modules = Mutex<[String: String?]>([:])

    /* The process's demangler, loaded the first time anyone asks. A load that failed is retried, since nothing was kept. */
    private static let loaded = Mutex<SwiftDemangler?>(nil)

    static func shared(runner: ProcessRunner = ProcessRunner()) throws -> SwiftDemangler {
        try loaded.withLock { demangler in
            if let demangler {
                return demangler
            }
            let created = try SwiftDemangler(libraryPath: toolchainLibraryPath(runner: runner))
            demangler = created
            return created
        }
    }

    init(libraryPath: String) throws {
        guard let handle = unsafe dlopen(libraryPath, RTLD_NOW | RTLD_LOCAL) else {
            let reason = unsafe dlerror().map { unsafe String(cString: $0) } ?? "no reason given"
            throw Failure(description: "could not load the demangler at \(libraryPath): \(reason)")
        }
        guard let address = unsafe dlsym(handle, "swift_demangle_getDemangledName") else {
            throw Failure(description: "the demangler at \(libraryPath) has no swift_demangle_getDemangledName")
        }
        guard let moduleAddress = unsafe dlsym(handle, "swift_demangle_getModuleName") else {
            throw Failure(description: "the demangler at \(libraryPath) has no swift_demangle_getModuleName")
        }
        /* Unsafe because each cast trusts the symbol to have the signature `GetDemangledName` spells, the one SwiftDemangle.h declares for both. */
        unsafe getDemangledName = unsafeBitCast(address, to: GetDemangledName.self)
        unsafe getModuleName = unsafeBitCast(moduleAddress, to: GetDemangledName.self)
    }

    /* The library beside the `swift` that `xcrun` resolves. */
    static func toolchainLibraryPath(runner: ProcessRunner = ProcessRunner()) throws -> String {
        let library = try SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner)
        return URL(fileURLWithPath: library).deletingLastPathComponent().appendingPathComponent(
            "libswiftDemangle.dylib"
        ).path
    }

    /*
     The declaration a unified symbol name spells, or nil for a name that is not a Swift declaration's (`c:` is
     C's and Objective-C's) or that does not demangle.
     */
    func declaration(ofSymbol symbol: String) -> String? {
        guard symbol.hasPrefix("s:") else { return nil }
        return demangle("$s" + symbol.dropFirst(2))
    }

    /*
     The module the declaration a unified symbol name spells is declared in, as the compiler's demangler reads it: for a
     member an extension adds, the extending module, wherever the extension's generic signature sits (#awb2ykk).
     `s:Sa7ControlSiRszlE5countSivp`, `count` in our `extension Array where Element == Int`, is `Control`'s, and
     `s:Sa5countSivp` is `Swift`'s. Nil for a name that is not a Swift declaration's or names no module.
     */
    func module(ofSymbol symbol: String) -> String? {
        guard symbol.hasPrefix("s:") else { return nil }
        if let known = modules.withLock({ $0[symbol] }) {
            return known
        }
        let module = unsafe call(getModuleName, "$s" + symbol.dropFirst(2)).flatMap { $0.isEmpty ? nil : $0 }
        modules.withLock { $0[symbol] = module }
        return module
    }

    /* A mangled name, `$s` and all, as the demangler prints it. */
    func demangle(_ mangled: String) -> String? {
        unsafe call(getDemangledName, mangled)
    }

    /* One of the library's functions on a mangled name. The first call says how long the answer is when the buffer was short. */
    private func call(_ function: GetDemangledName, _ mangled: String) -> String? {
        var capacity = 512
        while true {
            var buffer = [CChar](repeating: 0, count: capacity)
            /*
             The one call into the library, unsafe because a C function is trusted with a raw buffer. It stays in
             bounds because it is told the buffer's own count and writes no further, and the buffer is lent only
             for the call.

             The name and the buffer reach the C function through Swift's own argument conversions rather than
             `withCString` and `withUnsafeMutableBufferPointer` closures. Swift 6.3 counts those closure calls as
             unsafe and 6.4 does not, so no marking of them builds clean on both, while a single unsafe call does.
             */
            let length = unsafe function(mangled, &buffer, capacity)
            guard length > 0 else { return nil }
            if length < capacity {
                return String(decoding: buffer.prefix(length).map { UInt8(bitPattern: $0) }, as: UTF8.self)
            }
            capacity = length + 1
        }
    }
}
