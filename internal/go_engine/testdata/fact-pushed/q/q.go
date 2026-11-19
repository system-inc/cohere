// Package q holds, for the facts' reach (#5vbs1np, must 2), W, a printf wrapper on every platform but windows: it hands
// its format and values to platformLog, which each platform's file declares, and which forwards them to fmt.Printf on
// every platform but windows. So W's wrapper fact, which printf pushes to every importer, differs on windows, though
// nothing of W's signature does.
package q

// W hands its format and values to platformLog.
func W(format string, values ...any) {
	platformLog(format, values...)
}
