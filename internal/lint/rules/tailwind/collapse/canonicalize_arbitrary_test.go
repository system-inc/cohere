package tailwind

import (
	"strings"
	"testing"
)

// TestListedUniversesHoldUpstreamsProperties holds the index's grouping to upstream's signatures. A pool
// only signs the classes of groups whose universe admits the written class's properties, so a listed class
// whose real signature declares a property outside its group's universe is a class the pass never weighs,
// and the bail it should have caused goes missing. Every listed class on three themes, against the
// properties of upstream's own signature.
func TestListedUniversesHoldUpstreamsProperties(t *testing.T) {
	t.Parallel()
	for _, oracle := range signatureOracles {
		t.Run(oracle.name, func(t *testing.T) {
			t.Parallel()
			entryPoint, entries := readSignatureOracle(t, oracle.file)
			index := buildClassListIndex(oracleSystem(t, entryPoint))

			groupOf := map[string]*classListGroup{}
			for _, group := range index.groups {
				for _, className := range group.classes {
					groupOf[className] = group
				}
			}

			var outside []string
			checked := 0
			for _, entry := range entries {
				if entry.isTarget || entry.properties == "-" {
					continue
				}
				group, found := groupOf[entry.className]
				if !found {
					outside = append(outside, entry.className+": upstream lists it and the index does not")
					continue
				}
				checked++
				if group.maybe == nil {
					continue
				}
				for _, property := range strings.Fields(entry.properties) {
					if !group.maybe[property] {
						outside = append(outside, entry.className+": declares "+property+", outside its group's universe")
						break
					}
				}
			}
			if len(outside) > 0 {
				shown := firstPerRoot(outside)
				if len(shown) > 40 {
					shown = shown[:40]
				}
				t.Errorf("%d listed classes fall outside the index, starting with:\n  %s", len(outside), strings.Join(shown, "\n  "))
			}
			t.Logf("%d listed classes checked in %d groups", checked, len(index.groups))
		})
	}
}
