package native

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/prettier"
)

// TestUnregisteredTypesAreRefused keeps an absent printer from reading as a perfect one.
func TestUnregisteredTypesAreRefused(t *testing.T) {
	formatter := Formatter{Options: prettier.DefaultOptions()}
	if formatter.Handles("probe.unregistered") {
		t.Fatal("Handles said yes to a type nothing registered")
	}
	if _, err := formatter.Format("probe.unregistered", "x"); err == nil {
		t.Fatal("formatting a type nothing registered succeeded")
	}
}

// TestRegisteredPrinterReceivesTheOptions proves routing and that the resolved options reach the printer.
func TestRegisteredPrinterReceivesTheOptions(t *testing.T) {
	Register(".probe", func(fileName string, text string, options prettier.Options) (string, error) {
		return strings.Repeat(" ", options.TabWidth) + text, nil
	})
	defer func() { mutex.Lock(); delete(printers, ".probe"); mutex.Unlock() }()

	options := prettier.DefaultOptions()
	options.TabWidth = 3
	formatted, err := Formatter{Options: options}.Format("A.PROBE", "x")
	if err != nil {
		t.Fatal(err)
	}
	if formatted != "   x" {
		t.Fatalf("formatted %q, want the options' tab width applied by the registered printer", formatted)
	}
}

// TestRegisteringTwicePanics: two printers for one type would make output depend on init order.
func TestRegisteringTwicePanics(t *testing.T) {
	print := func(string, string, prettier.Options) (string, error) { return "", nil }
	Register(".twice", print)
	defer func() { mutex.Lock(); delete(printers, ".twice"); mutex.Unlock() }()
	defer func() {
		if recover() == nil {
			t.Fatal("registering .twice a second time did not panic")
		}
	}()
	Register(".twice", print)
}
