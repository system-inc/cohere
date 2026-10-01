/*
 The part of `swift package describe --type json` the engine reads. SwiftPM writes more; only these
 fields are decoded, so a new field in a later toolchain is ignored rather than fatal.
 */
struct DescribeOutput: Decodable {
    /* One target as `describe` writes it. `sources` are relative to `path`, and absent for some target kinds. */
    struct Target: Decodable {
        var name: String
        var type: String
        var path: String
        var sources: [String]?
    }

    /* A dependency. `fileSystem` ones carry an absolute `path`; `sourceControl` ones carry a URL the engine never reads. */
    struct Dependency: Decodable {
        var identity: String
        var type: String
        var path: String?
    }

    var name: String
    var targets: [Target]
    var dependencies: [Dependency]?
}
