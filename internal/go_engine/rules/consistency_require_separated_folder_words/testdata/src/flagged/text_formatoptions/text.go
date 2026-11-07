// want `^Folder name "text_formatoptions" runs its words together\. Use "text_format_options" instead\.`

// Each underscore-separated part is judged on its own, so a folder already separated in one place is still reported
// for a part that runs words together, and the spelling asked for keeps the parts it already has.
package text_formatoptions

// Value is something the package holds.
const Value = 1
