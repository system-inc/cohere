import Foundation
import Testing

@testable import CohereSwift

/*
 How one configuration's `-showBuildSettings` answer becomes what the toolchain rules judge, and how a target's
 configurations combine. The keys are the ones Xcode 27.2 printed for a fixture that set each (`XcodeBuildSettings`).
 */
struct XcodeBuildSettingsTests {
    static let allOn = [
        "SWIFT_VERSION": "6.0",
        "SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY": "YES",
        "SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY": "YES",
        "SWIFT_STRICT_MEMORY_SAFETY": "YES",
    ]

    @Test func theBuildSettingsTurnEachOn() throws {
        let read = try #require(XcodeBuildSettings.configuration(from: Self.allOn))
        #expect(read.languageMode == "6")
        #expect(read.upcomingFeatures == ["ExistentialAny", "MemberImportVisibility"])
        #expect(read.strictMemorySafety)
    }

    /* Absent is Xcode's default, `NO`; `MIGRATE` passes `<Name>:migrate`, which warns rather than turning the feature on. */
    @Test func absentNoAndMigrateAreOff() throws {
        let read = try #require(
            XcodeBuildSettings.configuration(from: [
                "SWIFT_VERSION": "5.0",
                "SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY": "MIGRATE",
                "SWIFT_STRICT_MEMORY_SAFETY": "NO",
            ])
        )
        #expect(read.languageMode == "5")
        #expect(read.upcomingFeatures.isEmpty)
        #expect(!read.strictMemorySafety)
    }

    @Test func otherSwiftFlagsTurnThemOnAndHaveTheLastWord() throws {
        let read = try #require(
            XcodeBuildSettings.configuration(from: [
                "SWIFT_VERSION": "6.0",
                "SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY": "YES",
                "OTHER_SWIFT_FLAGS":
                    "-D DEBUG -enable-upcoming-feature ExistentialAny -Xfrontend -strict-memory-safety -disable-upcoming-feature MemberImportVisibility -enable-upcoming-feature \"InferIsolatedConformances\"",
            ])
        )
        #expect(read.upcomingFeatures == ["ExistentialAny", "InferIsolatedConformances"])
        #expect(read.strictMemorySafety)
    }

    @Test func aMigratingFlagIsNotTheFeatureOn() throws {
        let read = try #require(
            XcodeBuildSettings.configuration(from: [
                "SWIFT_VERSION": "6.0",
                "OTHER_SWIFT_FLAGS": "-enable-upcoming-feature ExistentialAny:migrate -strict-memory-safety:migrate",
            ])
        )
        #expect(read.upcomingFeatures == ["ExistentialAny:migrate"])
        #expect(!read.strictMemorySafety)
    }

    /* Xcode compiles no Swift without a version, so a configuration that names none has no mode to judge. */
    @Test func noSwiftVersionIsNoAnswer() {
        #expect(XcodeBuildSettings.configuration(from: ["SWIFT_STRICT_MEMORY_SAFETY": "YES"]) == nil)
        #expect(XcodeBuildSettings.configuration(from: ["SWIFT_VERSION": " "]) == nil)
    }

    /* A setting on in Debug alone is not the target's: the target takes what every configuration has, and its oldest mode. */
    @Test func aTargetHasOnlyWhatEveryConfigurationHas() {
        let on = XcodeBuildSettings.Configuration(
            languageMode: "6",
            upcomingFeatures: ["ExistentialAny", "MemberImportVisibility"],
            strictMemorySafety: true,
        )
        let release = XcodeBuildSettings.Configuration(
            languageMode: "5",
            upcomingFeatures: ["ExistentialAny"],
            strictMemorySafety: false,
        )
        let root = URL(fileURLWithPath: "/project", isDirectory: true)
        var model = PackageModel(
            name: "Fixture",
            root: root,
            toolsVersion: "",
            targets: [
                PackageModel.Target(name: "App", kind: "application", directory: root, sources: [], languageMode: "6"),
                PackageModel.Target(
                    name: "Unread",
                    kind: "application",
                    directory: root,
                    sources: [],
                    languageMode: "6",
                ),
            ],
        )
        model.projectKind = .xcodeGen
        let settings = XcodeBuildSettings(
            definition: root.appendingPathComponent("project.yml"),
            targetLines: [:],
            targets: ["App": ["Debug": on, "Release": release]],
            unchecked: ["Unread": "it could not be read"],
        )
        let applied = settings.applied(to: model)
        let app = applied.targets.first { $0.name == "App" }
        #expect(app?.languageMode == "5")
        #expect(app?.upcomingFeatures == ["ExistentialAny"])
        #expect(app?.strictMemorySafety == false)
        #expect(applied.toolchainTargets.map(\.name) == ["App"], "a target nobody read is never judged")
        #expect(settings.configurations(of: "App", where: { !$0.strictMemorySafety }) == ["Release"])
        #expect(settings.configurations(of: "App", where: { _ in true }) == nil, "every configuration is no list")
    }

    @Test func listsReadAsASentence() {
        #expect(XcodeBuildSettings.listed(["Release"]) == "Release")
        #expect(XcodeBuildSettings.listed(["Debug", "Release"]) == "Debug and Release")
        #expect(XcodeBuildSettings.listed(["Debug", "Profile", "Release"]) == "Debug, Profile and Release")
    }

    /* The line that opens the target's object, never one of the lists that only refer to it. */
    @Test func aCommittedTargetsLineIsTheOneThatOpensItsObject() {
        let projectFile = """
            // !$*UTF8*$!
            {
            \tobjects = {
            \t\tPROJECT /* Project object */ = {
            \t\t\ttargets = (
            \t\t\t\tAPPTARGET /* App */,
            \t\t\t);
            \t\t};
            \t\tAPPTARGET /* App */ = {
            \t\t\tisa = PBXNativeTarget;
            \t\t};
            \t};
            }
            """
        let targets = [
            XcodeProjectFile.Target(
                name: "App",
                productType: "com.apple.product-type.application",
                sources: [],
                swiftVersion: nil,
                sdkRoot: nil,
                identifier: "APPTARGET",
            ),
            XcodeProjectFile.Target(
                name: "Gone",
                productType: "com.apple.product-type.application",
                sources: [],
                swiftVersion: nil,
                sdkRoot: nil,
                identifier: "ABSENT",
            ),
        ]
        #expect(XcodeBuildSettings.targetLines(inProjectFile: projectFile, targets: targets) == ["App": 9])
    }
}
