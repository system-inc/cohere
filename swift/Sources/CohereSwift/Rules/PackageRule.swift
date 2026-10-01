/*
 A rule about the package as a whole rather than any one file, such as which language mode its targets
 compile in. It gets the package model and the parsed manifest, so a finding can point at the line of
 `Package.swift` that would fix it.
 */
public protocol PackageRule: Sendable {
    var name: String { get }

    func findings(in package: PackageModel, manifest: ParsedFile?) -> [FindingRecord]
}
