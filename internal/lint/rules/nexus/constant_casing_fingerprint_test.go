package nexus

import (
	"crypto/sha256"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// constantCasingFingerprintOf is the rule's program fingerprint over a fixture, read through the Program the
// walk hands the rule, viewed under its own reads.
func constantCasingFingerprintOf(t *testing.T, files map[string]string, subject string) [sha256.Size]byte {
	t.Helper()
	var fingerprint [sha256.Size]byte
	probe := ConsistencyRequireConstantCasing
	probe.Run = func(ctx rule.Context, options any) rule.Listeners {
		fingerprint = ConsistencyRequireConstantCasing.ProgramFingerprint(ctx.Program)
		return nil
	}
	rule_testing.RunTypedFiles(t, probe, files, subject)
	if fingerprint == ([sha256.Size]byte{}) {
		t.Fatal("the probe never ran, so no fingerprint was read")
	}
	return fingerprint
}

// The fingerprint is the whole of what a file's verdict reads beyond the file and its types (#f96cnry),
// proven both ways. Whatever changes who takes which name from a file, or which directories a computed
// import roots, moves it. An edit that changes neither leaves it, so the findings cache replays across it.
func TestConsistencyRequireConstantCasingFingerprintsExactlyTheImporterIndex(t *testing.T) {
	t.Parallel()
	const source = "/repository/source/Source.ts"
	const reader = "/repository/source/Reader.ts"
	const other = "/repository/source/Other.ts"
	const loader = "/repository/source/Loader.ts"
	const mirror = "/repository/source/Mirror.ts"
	base := map[string]string{
		source: "export const archivePath = 'archive';\nexport const otherPath = 'other';\n",
		reader: "import { archivePath } from './Source';\nexport const Reading = archivePath;\n",
		loader: "export const load = (code: string) => import(`./translations/${code}`);\n",
		mirror: "export const archivePath = 'mirror';\n",
	}
	with := func(path string, text string) map[string]string {
		files := map[string]string{}
		for name, contents := range base {
			files[name] = contents
		}
		files[path] = text
		return files
	}
	original := constantCasingFingerprintOf(t, base, source)

	moves := map[string]map[string]string{
		"a new importer of an exported name": with(other, "import { otherPath } from './Source';\nexport const Other = otherPath;\n"),
		"an importer taking a second name": with(reader,
			"import { archivePath, otherPath } from './Source';\nexport const Reading = archivePath + otherPath;\n"),
		"an export star from an imported file":  with(other, "export * from './Source';\n"),
		"the same name taken from another file": with(reader, "import { archivePath } from './Mirror';\nexport const Reading = archivePath;\n"),
		"an importer taking a different name":   with(reader, "import { otherPath } from './Source';\nexport const Reading = otherPath;\n"),
		"a computed import pointed at another directory": with(loader,
			"export const load = (code: string) => import(`./locales/${code}`);\n"),
		"a computed import removed": with(loader, "export const load = (code: string) => code;\n"),
	}
	for name, files := range moves {
		if constantCasingFingerprintOf(t, files, source) == original {
			t.Errorf("%s left the fingerprint unchanged, so a stale finding would replay", name)
		}
	}

	holds := map[string]map[string]string{
		"a comment in the importer": with(reader,
			"// reads the archive\nimport { archivePath } from './Source';\nexport const Reading = archivePath;\n"),
		"an exported value changed": with(source, "export const archivePath = 'archives';\nexport const otherPath = 'other';\n"),
		"an import's local alias renamed": with(reader,
			"import { archivePath as path } from './Source';\nexport const Reading = path;\n"),
		"a file with a computed import edited elsewhere": with(loader,
			"export const load = (code: string) => import(`./translations/${code}`);\nexport const two = 2;\n"),
	}
	for name, files := range holds {
		if constantCasingFingerprintOf(t, files, source) != original {
			t.Errorf("%s moved the fingerprint, so files whose verdict cannot change would not replay", name)
		}
	}
}
