import CohereSwift
import Foundation
import Testing

/*
 The project file reader against project files xcodegen 2.x generated from small specs (each spec is named
 above its fixture), embedded as written so the test needs no xcodegen. Each fixture is written under a
 UUID-named folder at the relative place it was generated, with the Swift files it names, so its relative paths
 land on real files and a wrong resolution reads as a wrong path rather than as a passing one.
 */
@Suite
struct XcodeProjectFileTests {
    /* A fresh folder for one test, removed after it, with symlinks resolved so expected paths compare as the reader writes them. */
    static func withFolder(_ body: (URL) throws -> Void) throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-xcode-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer {
            do {
                try FileManager.default.removeItem(at: folder)
            }
            catch {
                /* Ignored on purpose: a leftover fixture in the temporary directory costs nothing, and failing a test that already answered would hide its answer. */
            }
        }
        try body(folder.resolvingSymlinksInPath())
    }

    static func write(_ text: String, _ relativePath: String, under folder: URL) throws {
        let file = folder.appendingPathComponent(relativePath)
        try FileManager.default.createDirectory(
            at: file.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try text.write(to: file, atomically: true, encoding: .utf8)
    }

    static func urls(_ relativePaths: [String], under folder: URL) -> [URL] {
        relativePaths.map { folder.appendingPathComponent($0).standardizedFileURL.resolvingSymlinksInPath() }
    }

    /* Two Swift files, one in a nested group, beside a resource that is not Swift and must not be listed. */
    @Test func readsAPlainSourcesFolder() throws {
        try Self.withFolder { folder in
            try Self.write(Self.plainProject, "plain/Plain.xcodeproj/project.pbxproj", under: folder)
            for file in ["Main.swift", "Nested/Model.swift", "Notes.txt"] {
                try Self.write("", "plain/Sources/App/\(file)", under: folder)
            }
            let project = try XcodeProjectFile.read(folder.appendingPathComponent("plain/Plain.xcodeproj"))
            #expect(project.sourceRoot.path == folder.appendingPathComponent("plain").path)
            #expect(
                project.targets == [
                    XcodeProjectFile.Target(
                        name: "App",
                        productType: "com.apple.product-type.tool",
                        sources: Self.urls(
                            ["plain/Sources/App/Main.swift", "plain/Sources/App/Nested/Model.swift"],
                            under: folder,
                        ),
                        swiftVersion: nil,
                        sdkRoot: "macosx",
                        identifier: "EECCF05A8AED383F33DCD2D1",
                        configurations: ["Debug", "Release"],
                    )
                ]
            )
        }
    }

    /*
     The application writes its own Swift version; the test bundle writes none and inherits the project's. The
     application's Debug configuration is given an SDK of its own here, so the bundle inheriting the project's shows
     the same precedence for `SDKROOT`.
     */
    @Test func readsEachTargetsSwiftVersionAndSdk() throws {
        try Self.withFolder { folder in
            let applicationDebug = "\t\t\t\tSWIFT_VERSION = 6.0;\n\t\t\t};\n\t\t\tname = Debug;"
            try #require(Self.twoTargetProject.components(separatedBy: applicationDebug).count == 2)
            try Self.write(
                Self.twoTargetProject.replacingOccurrences(
                    of: applicationDebug,
                    with: "\t\t\t\tSDKROOT = xros;\n" + applicationDebug,
                ),
                "two/Two.xcodeproj/project.pbxproj",
                under: folder,
            )
            try Self.write("", "two/App/App.swift", under: folder)
            try Self.write("", "two/Tests/AppTests.swift", under: folder)
            let project = try XcodeProjectFile.read(folder.appendingPathComponent("two/Two.xcodeproj"))
            #expect(
                project.targets == [
                    XcodeProjectFile.Target(
                        name: "App",
                        productType: "com.apple.product-type.application",
                        sources: Self.urls(["two/App/App.swift"], under: folder),
                        swiftVersion: "6.0",
                        sdkRoot: "xros",
                        identifier: "DEE546A52094D09E75881F9A",
                        configurations: ["Debug", "Release"],
                    ),
                    XcodeProjectFile.Target(
                        name: "AppTests",
                        productType: "com.apple.product-type.bundle.unit-test",
                        sources: Self.urls(["two/Tests/AppTests.swift"], under: folder),
                        swiftVersion: "5.0",
                        sdkRoot: "iphoneos",
                        identifier: "E33CCCDDA1AFB51B0450CCB9",
                        configurations: ["Debug", "Release"],
                    ),
                ]
            )
        }
    }

    /*
     Every Swift file under the synchronized folder, nested ones included, less the one the exception set takes
     out of the target. A hidden folder's Swift file and a file that is not Swift are on disk and not listed.
     */
    @Test func readsASynchronizedFolderLessItsExceptions() throws {
        try Self.withFolder { folder in
            try Self.write(Self.synchronizedProject, "synced/Synced.xcodeproj/project.pbxproj", under: folder)
            for file in ["Keep.swift", "Skip.swift", "Nested/Deep.swift", "Notes.txt", ".hidden/Ghost.swift"] {
                try Self.write("", "synced/Sources/\(file)", under: folder)
            }
            let project = try XcodeProjectFile.read(folder.appendingPathComponent("synced/Synced.xcodeproj"))
            #expect(project.targets.map(\.name) == ["Tool"])
            #expect(
                project.targets.first?.sources
                    == Self.urls(["synced/Sources/Keep.swift", "synced/Sources/Nested/Deep.swift"], under: folder)
            )
        }
    }

    /*
     A project generated away from its spec: the group's path climbs out of the project's directory
     (`../../work/Sources`), and the source root is the directory holding the .xcodeproj, not the spec's.
     */
    @Test func resolvesPathsOutsideTheProjectDirectory() throws {
        try Self.withFolder { folder in
            try Self.write(
                Self.elsewhereProject,
                "elsewhere/build/out/Elsewhere.xcodeproj/project.pbxproj",
                under: folder,
            )
            try Self.write("", "elsewhere/work/Sources/Away.swift", under: folder)
            let project = try XcodeProjectFile.read(
                folder.appendingPathComponent("elsewhere/build/out/Elsewhere.xcodeproj")
            )
            #expect(project.sourceRoot.path == folder.appendingPathComponent("elsewhere/build/out").path)
            #expect(project.targets.map(\.productType) == ["com.apple.product-type.framework"])
            #expect(project.targets.first?.sources == Self.urls(["elsewhere/work/Sources/Away.swift"], under: folder))
        }
    }

    /*
     A package reference counted from the folder holding the .xcodeproj, and a folder reference holding a
     `Package.swift`, are both local packages. A folder reference without one is a folder of resources, not listed.
     */
    @Test func readsTheLocalPackagesItLinks() throws {
        try Self.withFolder { folder in
            let section = "/* Begin PBXBuildFile section */\n"
            let references = [
                "AAAA00000000000000000001 = {isa = XCLocalSwiftPackageReference; relativePath = ../Near; };",
                "AAAA00000000000000000002 = {isa = PBXFileReference; lastKnownFileType = folder; path = ../Far; sourceTree = SOURCE_ROOT; };",
                "AAAA00000000000000000003 = {isa = PBXFileReference; lastKnownFileType = folder; path = Assets; sourceTree = SOURCE_ROOT; };",
            ]
            try Self.write(
                Self.plainProject.replacingOccurrences(
                    of: section,
                    with: section + references.map { "\t\t\($0)\n" }.joined(),
                ),
                "plain/Plain.xcodeproj/project.pbxproj",
                under: folder,
            )
            for file in ["Near/Package.swift", "Far/Package.swift", "plain/Assets/Notes.txt"] {
                try Self.write("", file, under: folder)
            }
            let project = try XcodeProjectFile.read(folder.appendingPathComponent("plain/Plain.xcodeproj"))
            #expect(project.localPackages == Self.urls(["Far", "Near"], under: folder))
        }
    }

    /* Text that is not a property list, one with no rootObject, and one naming an object it does not hold: each refused with the path, never read as an empty project. */
    @Test(arguments: [
        ("{ objects = ( ; }", "it is not a property list"),
        (
            XcodeProjectFileTests.plainProject.replacingOccurrences(of: "rootObject", with: "rootless"),
            "it names no rootObject",
        ),
        (
            XcodeProjectFileTests.plainProject.replacingOccurrences(
                of: "fileRef = 00D1BF663A887A78C38CC1C6",
                with: "fileRef = 000000000000000000000000",
            ),
            "000000000000000000000000, the file of build file 66CBDC9E89AA0BBA7DDECE81, is not among its objects",
        ),
    ])
    func refusesWhatItCannotRead(text: String, reason: String) throws {
        try Self.withFolder { folder in
            try Self.write(text, "broken/Broken.xcodeproj/project.pbxproj", under: folder)
            let bundle = folder.appendingPathComponent("broken/Broken.xcodeproj")
            let failure = #expect(throws: XcodeProjectFile.ReadFailure.self) {
                try XcodeProjectFile.read(bundle)
            }
            #expect(failure?.description.contains(reason) == true, "\(failure?.description ?? "no failure")")
            #expect(failure?.description.contains(bundle.appendingPathComponent("project.pbxproj").path) == true)
        }
    }

    /* `plain/project.yml`: a macOS tool whose sources are `Sources/App`, holding `Main.swift`, `Notes.txt` and `Nested/Model.swift`. */
    static let plainProject = #"""
        // !$*UTF8*$!
        {
        	archiveVersion = 1;
        	classes = {
        	};
        	objectVersion = 77;
        	objects = {

        /* Begin PBXBuildFile section */
        		3C11EB4489A94CA34A58874F /* Model.swift in Sources */ = {isa = PBXBuildFile; fileRef = 6E356F874CA34A01F575255F /* Model.swift */; };
        		66CBDC9E89AA0BBA7DDECE81 /* Main.swift in Sources */ = {isa = PBXBuildFile; fileRef = 00D1BF663A887A78C38CC1C6 /* Main.swift */; };
        		CCE6E2FA4B2A1BC4B59E5C25 /* Notes.txt in Resources */ = {isa = PBXBuildFile; fileRef = 7A8896C8D3937DEA3A7419D5 /* Notes.txt */; };
        /* End PBXBuildFile section */

        /* Begin PBXFileReference section */
        		00D1BF663A887A78C38CC1C6 /* Main.swift */ = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = Main.swift; sourceTree = "<group>"; };
        		3C8149E39F5A2EE7E14AF342 /* App */ = {isa = PBXFileReference; includeInIndex = 0; path = App; sourceTree = BUILT_PRODUCTS_DIR; };
        		6E356F874CA34A01F575255F /* Model.swift */ = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = Model.swift; sourceTree = "<group>"; };
        		7A8896C8D3937DEA3A7419D5 /* Notes.txt */ = {isa = PBXFileReference; lastKnownFileType = text; path = Notes.txt; sourceTree = "<group>"; };
        /* End PBXFileReference section */

        /* Begin PBXGroup section */
        		0AE9AAB9F38857756A433A5D /* App */ = {
        			isa = PBXGroup;
        			children = (
        				00D1BF663A887A78C38CC1C6 /* Main.swift */,
        				7A8896C8D3937DEA3A7419D5 /* Notes.txt */,
        				5ED6A2E5D2D1A68BED57882C /* Nested */,
        			);
        			name = App;
        			path = Sources/App;
        			sourceTree = "<group>";
        		};
        		5ED6A2E5D2D1A68BED57882C /* Nested */ = {
        			isa = PBXGroup;
        			children = (
        				6E356F874CA34A01F575255F /* Model.swift */,
        			);
        			path = Nested;
        			sourceTree = "<group>";
        		};
        		AFCA76DD7C4653903B8101C0 = {
        			isa = PBXGroup;
        			children = (
        				0AE9AAB9F38857756A433A5D /* App */,
        				C35D05B9FC67101C9732CE2A /* Products */,
        			);
        			sourceTree = "<group>";
        		};
        		C35D05B9FC67101C9732CE2A /* Products */ = {
        			isa = PBXGroup;
        			children = (
        				3C8149E39F5A2EE7E14AF342 /* App */,
        			);
        			name = Products;
        			sourceTree = "<group>";
        		};
        /* End PBXGroup section */

        /* Begin PBXNativeTarget section */
        		EECCF05A8AED383F33DCD2D1 /* App */ = {
        			isa = PBXNativeTarget;
        			buildConfigurationList = 0B3250BF02BDEF98B2268FE7 /* Build configuration list for PBXNativeTarget "App" */;
        			buildPhases = (
        				7073CA1B99026A254053DE0E /* Sources */,
        				D2EFDEA6A8B95DA5D4EF2DD4 /* Resources */,
        			);
        			buildRules = (
        			);
        			dependencies = (
        			);
        			name = App;
        			packageProductDependencies = (
        			);
        			productName = App;
        			productReference = 3C8149E39F5A2EE7E14AF342 /* App */;
        			productType = "com.apple.product-type.tool";
        		};
        /* End PBXNativeTarget section */

        /* Begin PBXProject section */
        		1CE545037161400820E47F68 /* Project object */ = {
        			isa = PBXProject;
        			attributes = {
        				BuildIndependentTargetsInParallel = YES;
        				LastUpgradeCheck = 1430;
        				TargetAttributes = {
        				};
        			};
        			buildConfigurationList = 101771FB2CD5B0EBFF7E6F52 /* Build configuration list for PBXProject "Plain" */;
        			developmentRegion = en;
        			hasScannedForEncodings = 0;
        			knownRegions = (
        				Base,
        				en,
        			);
        			mainGroup = AFCA76DD7C4653903B8101C0;
        			minimizedProjectReferenceProxies = 1;
        			preferredProjectObjectVersion = 77;
        			productRefGroup = C35D05B9FC67101C9732CE2A /* Products */;
        			projectDirPath = "";
        			projectRoot = "";
        			targets = (
        				EECCF05A8AED383F33DCD2D1 /* App */,
        			);
        		};
        /* End PBXProject section */

        /* Begin PBXResourcesBuildPhase section */
        		D2EFDEA6A8B95DA5D4EF2DD4 /* Resources */ = {
        			isa = PBXResourcesBuildPhase;
        			buildActionMask = 2147483647;
        			files = (
        				CCE6E2FA4B2A1BC4B59E5C25 /* Notes.txt in Resources */,
        			);
        			runOnlyForDeploymentPostprocessing = 0;
        		};
        /* End PBXResourcesBuildPhase section */

        /* Begin PBXSourcesBuildPhase section */
        		7073CA1B99026A254053DE0E /* Sources */ = {
        			isa = PBXSourcesBuildPhase;
        			buildActionMask = 2147483647;
        			files = (
        				66CBDC9E89AA0BBA7DDECE81 /* Main.swift in Sources */,
        				3C11EB4489A94CA34A58874F /* Model.swift in Sources */,
        			);
        			runOnlyForDeploymentPostprocessing = 0;
        		};
        /* End PBXSourcesBuildPhase section */

        /* Begin XCBuildConfiguration section */
        		1407C3CDDED3C56E7B6FFDB5 /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        			};
        			name = Release;
        		};
        		5B3298E508409319730370B0 /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = macosx;
        			};
        			name = Release;
        		};
        		6F34C5BEA984E8AAB6CF0F47 /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        			};
        			name = Debug;
        		};
        		9C9EF0F4FED1DE86A7FC1006 /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = macosx;
        			};
        			name = Debug;
        		};
        /* End XCBuildConfiguration section */

        /* Begin XCConfigurationList section */
        		0B3250BF02BDEF98B2268FE7 /* Build configuration list for PBXNativeTarget "App" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				6F34C5BEA984E8AAB6CF0F47 /* Debug */,
        				1407C3CDDED3C56E7B6FFDB5 /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        		101771FB2CD5B0EBFF7E6F52 /* Build configuration list for PBXProject "Plain" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				9C9EF0F4FED1DE86A7FC1006 /* Debug */,
        				5B3298E508409319730370B0 /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        /* End XCConfigurationList section */
        	};
        	rootObject = 1CE545037161400820E47F68 /* Project object */;
        }
        """#

    /* `two/project.yml`: an iOS application writing `SWIFT_VERSION: "6.0"` and a unit-test bundle writing none, under a project writing `"5.0"`. */
    static let twoTargetProject = #"""
        // !$*UTF8*$!
        {
        	archiveVersion = 1;
        	classes = {
        	};
        	objectVersion = 77;
        	objects = {

        /* Begin PBXBuildFile section */
        		1166C17DC5772A047FCD2759 /* App.swift in Sources */ = {isa = PBXBuildFile; fileRef = A59C9E0EF224C85CFEA39E54 /* App.swift */; };
        		C7E2F2E54A55DCB5924090EF /* AppTests.swift in Sources */ = {isa = PBXBuildFile; fileRef = C8A18B50098EACA986E276AF /* AppTests.swift */; };
        /* End PBXBuildFile section */

        /* Begin PBXContainerItemProxy section */
        		B8615FB96870C299B9611309 /* PBXContainerItemProxy */ = {
        			isa = PBXContainerItemProxy;
        			containerPortal = 6F90DA9CE859ABDB91C96792 /* Project object */;
        			proxyType = 1;
        			remoteGlobalIDString = DEE546A52094D09E75881F9A;
        			remoteInfo = App;
        		};
        /* End PBXContainerItemProxy section */

        /* Begin PBXFileReference section */
        		1712C971FE6E5AAABDCF45D5 /* AppTests.xctest */ = {isa = PBXFileReference; includeInIndex = 0; lastKnownFileType = wrapper.cfbundle; path = AppTests.xctest; sourceTree = BUILT_PRODUCTS_DIR; };
        		A59C9E0EF224C85CFEA39E54 /* App.swift */ = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = App.swift; sourceTree = "<group>"; };
        		C8A18B50098EACA986E276AF /* AppTests.swift */ = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = AppTests.swift; sourceTree = "<group>"; };
        		E51C2796AECD2CC0E01885D6 /* App.app */ = {isa = PBXFileReference; includeInIndex = 0; lastKnownFileType = wrapper.application; path = App.app; sourceTree = BUILT_PRODUCTS_DIR; };
        /* End PBXFileReference section */

        /* Begin PBXGroup section */
        		186FF64009E16266AA872584 /* Tests */ = {
        			isa = PBXGroup;
        			children = (
        				C8A18B50098EACA986E276AF /* AppTests.swift */,
        			);
        			path = Tests;
        			sourceTree = "<group>";
        		};
        		1B1E907550703A6A06EE5F53 /* Products */ = {
        			isa = PBXGroup;
        			children = (
        				E51C2796AECD2CC0E01885D6 /* App.app */,
        				1712C971FE6E5AAABDCF45D5 /* AppTests.xctest */,
        			);
        			name = Products;
        			sourceTree = "<group>";
        		};
        		275C36AE09B45B88B5E141BF = {
        			isa = PBXGroup;
        			children = (
        				6E92F84BFD25FE3286D8DFA7 /* App */,
        				186FF64009E16266AA872584 /* Tests */,
        				1B1E907550703A6A06EE5F53 /* Products */,
        			);
        			sourceTree = "<group>";
        		};
        		6E92F84BFD25FE3286D8DFA7 /* App */ = {
        			isa = PBXGroup;
        			children = (
        				A59C9E0EF224C85CFEA39E54 /* App.swift */,
        			);
        			path = App;
        			sourceTree = "<group>";
        		};
        /* End PBXGroup section */

        /* Begin PBXNativeTarget section */
        		DEE546A52094D09E75881F9A /* App */ = {
        			isa = PBXNativeTarget;
        			buildConfigurationList = 232D753774E9029B6C889A29 /* Build configuration list for PBXNativeTarget "App" */;
        			buildPhases = (
        				08A609DDB8765AA4CF937980 /* Sources */,
        			);
        			buildRules = (
        			);
        			dependencies = (
        			);
        			name = App;
        			packageProductDependencies = (
        			);
        			productName = App;
        			productReference = E51C2796AECD2CC0E01885D6 /* App.app */;
        			productType = "com.apple.product-type.application";
        		};
        		E33CCCDDA1AFB51B0450CCB9 /* AppTests */ = {
        			isa = PBXNativeTarget;
        			buildConfigurationList = 9727790C9A8D06B5EA6E0520 /* Build configuration list for PBXNativeTarget "AppTests" */;
        			buildPhases = (
        				AD9E799401C2385BD0DAA6F2 /* Sources */,
        			);
        			buildRules = (
        			);
        			dependencies = (
        				43785EF730DE65D3024A2C26 /* PBXTargetDependency */,
        			);
        			name = AppTests;
        			packageProductDependencies = (
        			);
        			productName = AppTests;
        			productReference = 1712C971FE6E5AAABDCF45D5 /* AppTests.xctest */;
        			productType = "com.apple.product-type.bundle.unit-test";
        		};
        /* End PBXNativeTarget section */

        /* Begin PBXProject section */
        		6F90DA9CE859ABDB91C96792 /* Project object */ = {
        			isa = PBXProject;
        			attributes = {
        				BuildIndependentTargetsInParallel = YES;
        				LastUpgradeCheck = 1430;
        				TargetAttributes = {
        				};
        			};
        			buildConfigurationList = 6D6F475FD795FDFE4D35E467 /* Build configuration list for PBXProject "Two" */;
        			developmentRegion = en;
        			hasScannedForEncodings = 0;
        			knownRegions = (
        				Base,
        				en,
        			);
        			mainGroup = 275C36AE09B45B88B5E141BF;
        			minimizedProjectReferenceProxies = 1;
        			preferredProjectObjectVersion = 77;
        			productRefGroup = 1B1E907550703A6A06EE5F53 /* Products */;
        			projectDirPath = "";
        			projectRoot = "";
        			targets = (
        				DEE546A52094D09E75881F9A /* App */,
        				E33CCCDDA1AFB51B0450CCB9 /* AppTests */,
        			);
        		};
        /* End PBXProject section */

        /* Begin PBXSourcesBuildPhase section */
        		08A609DDB8765AA4CF937980 /* Sources */ = {
        			isa = PBXSourcesBuildPhase;
        			buildActionMask = 2147483647;
        			files = (
        				1166C17DC5772A047FCD2759 /* App.swift in Sources */,
        			);
        			runOnlyForDeploymentPostprocessing = 0;
        		};
        		AD9E799401C2385BD0DAA6F2 /* Sources */ = {
        			isa = PBXSourcesBuildPhase;
        			buildActionMask = 2147483647;
        			files = (
        				C7E2F2E54A55DCB5924090EF /* AppTests.swift in Sources */,
        			);
        			runOnlyForDeploymentPostprocessing = 0;
        		};
        /* End PBXSourcesBuildPhase section */

        /* Begin PBXTargetDependency section */
        		43785EF730DE65D3024A2C26 /* PBXTargetDependency */ = {
        			isa = PBXTargetDependency;
        			target = DEE546A52094D09E75881F9A /* App */;
        			targetProxy = B8615FB96870C299B9611309 /* PBXContainerItemProxy */;
        		};
        /* End PBXTargetDependency section */

        /* Begin XCBuildConfiguration section */
        		3C60DD544E6156CC0B0B75BA /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = iphoneos;
        				SWIFT_VERSION = 5.0;
        			};
        			name = Debug;
        		};
        		667EF30D25647A4B368F9E57 /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SWIFT_VERSION = 6.0;
        			};
        			name = Release;
        		};
        		749B772C38A8A8067D4F5260 /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = iphoneos;
        				SWIFT_VERSION = 5.0;
        			};
        			name = Release;
        		};
        		833B01875CDFB4CF54CFA2FB /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SWIFT_VERSION = 6.0;
        			};
        			name = Debug;
        		};
        		C347D6F820345223B9A1067C /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				TEST_HOST = "$(BUILT_PRODUCTS_DIR)/App.app/App";
        			};
        			name = Debug;
        		};
        		C6C464889250D78B41B2BC17 /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				TEST_HOST = "$(BUILT_PRODUCTS_DIR)/App.app/App";
        			};
        			name = Release;
        		};
        /* End XCBuildConfiguration section */

        /* Begin XCConfigurationList section */
        		232D753774E9029B6C889A29 /* Build configuration list for PBXNativeTarget "App" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				833B01875CDFB4CF54CFA2FB /* Debug */,
        				667EF30D25647A4B368F9E57 /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        		6D6F475FD795FDFE4D35E467 /* Build configuration list for PBXProject "Two" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				3C60DD544E6156CC0B0B75BA /* Debug */,
        				749B772C38A8A8067D4F5260 /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        		9727790C9A8D06B5EA6E0520 /* Build configuration list for PBXNativeTarget "AppTests" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				C347D6F820345223B9A1067C /* Debug */,
        				C6C464889250D78B41B2BC17 /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        /* End XCConfigurationList section */
        	};
        	rootObject = 6F90DA9CE859ABDB91C96792 /* Project object */;
        }
        """#

    /* `synced/project.yml`: a macOS tool whose sources are `Sources` as a `syncedFolder` with `excludes: [Skip.swift]`. */
    static let synchronizedProject = #"""
        // !$*UTF8*$!
        {
        	archiveVersion = 1;
        	classes = {
        	};
        	objectVersion = 77;
        	objects = {

        /* Begin PBXFileReference section */
        		00E1A39A86D081F841A24016 /* Tool */ = {isa = PBXFileReference; includeInIndex = 0; path = Tool; sourceTree = BUILT_PRODUCTS_DIR; };
        /* End PBXFileReference section */

        /* Begin PBXFileSystemSynchronizedBuildFileExceptionSet section */
        		F4C70A310D58D048C436962C /* PBXFileSystemSynchronizedBuildFileExceptionSet */ = {
        			isa = PBXFileSystemSynchronizedBuildFileExceptionSet;
        			membershipExceptions = (
        				Skip.swift,
        			);
        			target = 2DA892E8B757D30BA8E12D0F /* Tool */;
        		};
        /* End PBXFileSystemSynchronizedBuildFileExceptionSet section */

        /* Begin PBXFileSystemSynchronizedRootGroup section */
        		9AFA97DB61FCFC8DC6496C03 /* Sources */ = {
        			isa = PBXFileSystemSynchronizedRootGroup;
        			exceptions = (
        				F4C70A310D58D048C436962C /* PBXFileSystemSynchronizedBuildFileExceptionSet */,
        			);
        			explicitFileTypes = {
        			};
        			explicitFolders = (
        			);
        			path = Sources;
        			sourceTree = "<group>";
        		};
        /* End PBXFileSystemSynchronizedRootGroup section */

        /* Begin PBXGroup section */
        		0EB5BC326DB4672BC13B946D /* Products */ = {
        			isa = PBXGroup;
        			children = (
        				00E1A39A86D081F841A24016 /* Tool */,
        			);
        			name = Products;
        			sourceTree = "<group>";
        		};
        		3F883BAD62BB47E25533F40E = {
        			isa = PBXGroup;
        			children = (
        				9AFA97DB61FCFC8DC6496C03 /* Sources */,
        				0EB5BC326DB4672BC13B946D /* Products */,
        			);
        			sourceTree = "<group>";
        		};
        /* End PBXGroup section */

        /* Begin PBXNativeTarget section */
        		2DA892E8B757D30BA8E12D0F /* Tool */ = {
        			isa = PBXNativeTarget;
        			buildConfigurationList = 799BE06FAA8D596BD42826FA /* Build configuration list for PBXNativeTarget "Tool" */;
        			buildPhases = (
        				BD3F6287534B754544FE76FA /* Sources */,
        				E32F23720AEACB9ED4ACD35A /* Resources */,
        			);
        			buildRules = (
        			);
        			dependencies = (
        			);
        			fileSystemSynchronizedGroups = (
        				9AFA97DB61FCFC8DC6496C03 /* Sources */,
        			);
        			name = Tool;
        			packageProductDependencies = (
        			);
        			productName = Tool;
        			productReference = 00E1A39A86D081F841A24016 /* Tool */;
        			productType = "com.apple.product-type.tool";
        		};
        /* End PBXNativeTarget section */

        /* Begin PBXProject section */
        		217033FABC2192F01C59409C /* Project object */ = {
        			isa = PBXProject;
        			attributes = {
        				BuildIndependentTargetsInParallel = YES;
        				LastUpgradeCheck = 1430;
        				TargetAttributes = {
        				};
        			};
        			buildConfigurationList = CB1167800A963BB34B435BF9 /* Build configuration list for PBXProject "Synced" */;
        			developmentRegion = en;
        			hasScannedForEncodings = 0;
        			knownRegions = (
        				Base,
        				en,
        			);
        			mainGroup = 3F883BAD62BB47E25533F40E;
        			minimizedProjectReferenceProxies = 1;
        			preferredProjectObjectVersion = 77;
        			productRefGroup = 0EB5BC326DB4672BC13B946D /* Products */;
        			projectDirPath = "";
        			projectRoot = "";
        			targets = (
        				2DA892E8B757D30BA8E12D0F /* Tool */,
        			);
        		};
        /* End PBXProject section */

        /* Begin PBXResourcesBuildPhase section */
        		E32F23720AEACB9ED4ACD35A /* Resources */ = {
        			isa = PBXResourcesBuildPhase;
        			buildActionMask = 2147483647;
        			files = (
        			);
        			runOnlyForDeploymentPostprocessing = 0;
        		};
        /* End PBXResourcesBuildPhase section */

        /* Begin PBXSourcesBuildPhase section */
        		BD3F6287534B754544FE76FA /* Sources */ = {
        			isa = PBXSourcesBuildPhase;
        			buildActionMask = 2147483647;
        			files = (
        			);
        			runOnlyForDeploymentPostprocessing = 0;
        		};
        /* End PBXSourcesBuildPhase section */

        /* Begin XCBuildConfiguration section */
        		3AB532BEAD7D48C2EEC2034D /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        			};
        			name = Release;
        		};
        		487AF0234AB30CC3A8F6DF16 /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        			};
        			name = Debug;
        		};
        		EA31EC24CB488CE8DB48FB0D /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = macosx;
        			};
        			name = Debug;
        		};
        		F46C691FEF9BEB14792C1751 /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = macosx;
        			};
        			name = Release;
        		};
        /* End XCBuildConfiguration section */

        /* Begin XCConfigurationList section */
        		799BE06FAA8D596BD42826FA /* Build configuration list for PBXNativeTarget "Tool" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				487AF0234AB30CC3A8F6DF16 /* Debug */,
        				3AB532BEAD7D48C2EEC2034D /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        		CB1167800A963BB34B435BF9 /* Build configuration list for PBXProject "Synced" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				EA31EC24CB488CE8DB48FB0D /* Debug */,
        				F46C691FEF9BEB14792C1751 /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        /* End XCConfigurationList section */
        	};
        	rootObject = 217033FABC2192F01C59409C /* Project object */;
        }
        """#

    /* `elsewhere/work/project.yml`, generated with `--project elsewhere/build/out`: a macOS framework whose sources are `Sources` beside the spec. */
    static let elsewhereProject = #"""
        // !$*UTF8*$!
        {
        	archiveVersion = 1;
        	classes = {
        	};
        	objectVersion = 77;
        	objects = {

        /* Begin PBXBuildFile section */
        		17B72C84AB5899290CED644C /* Away.swift in Sources */ = {isa = PBXBuildFile; fileRef = EA20F974585A1D5F2BBFDF9C /* Away.swift */; };
        /* End PBXBuildFile section */

        /* Begin PBXFileReference section */
        		118412392952B4AB6B60E791 /* Away.framework */ = {isa = PBXFileReference; explicitFileType = wrapper.framework; includeInIndex = 0; path = Away.framework; sourceTree = BUILT_PRODUCTS_DIR; };
        		EA20F974585A1D5F2BBFDF9C /* Away.swift */ = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = Away.swift; sourceTree = "<group>"; };
        /* End PBXFileReference section */

        /* Begin PBXGroup section */
        		26644CC90AA9A2B5B4552BAB = {
        			isa = PBXGroup;
        			children = (
        				DC4509258B2881C9B631F3F7 /* Sources */,
        				AE76A559C72FBF3E640A7482 /* Products */,
        			);
        			sourceTree = "<group>";
        		};
        		AE76A559C72FBF3E640A7482 /* Products */ = {
        			isa = PBXGroup;
        			children = (
        				118412392952B4AB6B60E791 /* Away.framework */,
        			);
        			name = Products;
        			sourceTree = "<group>";
        		};
        		DC4509258B2881C9B631F3F7 /* Sources */ = {
        			isa = PBXGroup;
        			children = (
        				EA20F974585A1D5F2BBFDF9C /* Away.swift */,
        			);
        			name = Sources;
        			path = ../../work/Sources;
        			sourceTree = "<group>";
        		};
        /* End PBXGroup section */

        /* Begin PBXNativeTarget section */
        		7BB295B7055CE085C89D24FF /* Away */ = {
        			isa = PBXNativeTarget;
        			buildConfigurationList = 6A7216747B5A1003D7ED31B5 /* Build configuration list for PBXNativeTarget "Away" */;
        			buildPhases = (
        				9C511AC29930A212CF2B5DFA /* Sources */,
        			);
        			buildRules = (
        			);
        			dependencies = (
        			);
        			name = Away;
        			packageProductDependencies = (
        			);
        			productName = Away;
        			productReference = 118412392952B4AB6B60E791 /* Away.framework */;
        			productType = "com.apple.product-type.framework";
        		};
        /* End PBXNativeTarget section */

        /* Begin PBXProject section */
        		D2B7FBE59C10B77053D9B33A /* Project object */ = {
        			isa = PBXProject;
        			attributes = {
        				BuildIndependentTargetsInParallel = YES;
        				LastUpgradeCheck = 1430;
        				TargetAttributes = {
        				};
        			};
        			buildConfigurationList = 169128FC46A2958C1ABC2199 /* Build configuration list for PBXProject "Elsewhere" */;
        			developmentRegion = en;
        			hasScannedForEncodings = 0;
        			knownRegions = (
        				Base,
        				en,
        			);
        			mainGroup = 26644CC90AA9A2B5B4552BAB;
        			minimizedProjectReferenceProxies = 1;
        			preferredProjectObjectVersion = 77;
        			productRefGroup = AE76A559C72FBF3E640A7482 /* Products */;
        			projectDirPath = "";
        			projectRoot = "";
        			targets = (
        				7BB295B7055CE085C89D24FF /* Away */,
        			);
        		};
        /* End PBXProject section */

        /* Begin PBXSourcesBuildPhase section */
        		9C511AC29930A212CF2B5DFA /* Sources */ = {
        			isa = PBXSourcesBuildPhase;
        			buildActionMask = 2147483647;
        			files = (
        				17B72C84AB5899290CED644C /* Away.swift in Sources */,
        			);
        			runOnlyForDeploymentPostprocessing = 0;
        		};
        /* End PBXSourcesBuildPhase section */

        /* Begin XCBuildConfiguration section */
        		39914F6C4FF2CC1B477468C4 /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        			};
        			name = Debug;
        		};
        		45EF6E7D7771C65FE0D4DA61 /* Debug */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = macosx;
        			};
        			name = Debug;
        		};
        		937895EC1397F799FEBA8CFB /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        				SDKROOT = macosx;
        			};
        			name = Release;
        		};
        		D0F507C4F728BF82D5B99887 /* Release */ = {
        			isa = XCBuildConfiguration;
        			buildSettings = {
        			};
        			name = Release;
        		};
        /* End XCBuildConfiguration section */

        /* Begin XCConfigurationList section */
        		169128FC46A2958C1ABC2199 /* Build configuration list for PBXProject "Elsewhere" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				45EF6E7D7771C65FE0D4DA61 /* Debug */,
        				937895EC1397F799FEBA8CFB /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        		6A7216747B5A1003D7ED31B5 /* Build configuration list for PBXNativeTarget "Away" */ = {
        			isa = XCConfigurationList;
        			buildConfigurations = (
        				39914F6C4FF2CC1B477468C4 /* Debug */,
        				D0F507C4F728BF82D5B99887 /* Release */,
        			);
        			defaultConfigurationIsVisible = 0;
        			defaultConfigurationName = Debug;
        		};
        /* End XCConfigurationList section */
        	};
        	rootObject = D2B7FBE59C10B77053D9B33A /* Project object */;
        }
        """#
}
