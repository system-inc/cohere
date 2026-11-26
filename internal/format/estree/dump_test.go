package estree

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/json_value"
)

// jsonString is a Go string as JSON carries it: encoding/json writes each byte that is not UTF-8 as U+FFFD,
// and converting to runes replaces each such byte the same way, so a dump holds the string it would after a
// round trip through JSON.
func jsonString(text string) json_value.Value {
	return json_value.String(string([]rune(text)))
}

// jsonInteger is an int as a JSON number.
func jsonInteger(number int) json_value.Value {
	return json_value.Number(strconv.Itoa(number))
}

// dumpValue is a property's value as JSON: a node as its type, its range through offsets and its properties,
// leaving out its comments, its tokens and the properties named with a leading __ but __contentEnd.
func dumpValue(value Value, offsets []int) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.Null{}
	case *Node:
		if typed == nil {
			return json_value.Null{}
		}
		result := json_value.Object{
			"type":  jsonString(typed.Type()),
			"range": json_value.Array{jsonInteger(offsets[typed.Range[0]]), jsonInteger(offsets[typed.Range[1]])},
		}
		if typed.HasContentEnd {
			result["__contentEnd"] = jsonInteger(offsets[typed.ContentEnd])
		}
		keys := typed.Keys()
		sort.Strings(keys)
		for _, key := range keys {
			if key == "comments" || key == "tokens" || strings.HasPrefix(key, "__") {
				continue
			}
			result[key] = dumpValue(typed.Get(key), offsets)
		}
		return result
	case Nodes:
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpValue(child, offsets)
		}
		return result
	case Values:
		result := make(json_value.Array, len(typed))
		for index, element := range typed {
			result[index] = dumpValue(element, offsets)
		}
		return result
	case Record:
		result := make(json_value.Object, len(typed))
		for key, member := range typed {
			result[key] = dumpValue(member, offsets)
		}
		return result
	case *TemplateValue:
		var cooked json_value.Value = json_value.Null{}
		if typed.Cooked != nil {
			cooked = jsonString(*typed.Cooked)
		}
		return json_value.Object{"cooked": cooked, "raw": jsonString(typed.Raw)}
	case *Regex:
		return json_value.Object{"flags": jsonString(typed.Flags), "pattern": jsonString(typed.Pattern)}
	case String:
		return jsonString(string(typed))
	case Boolean:
		return json_value.Boolean(typed)
	case Number:
		// NaN and the infinities have no JSON spelling; encoding/json refused them where this dump went
		// through it, and json.Marshal still refuses this Number, so a convertedDump fails alike.
		return json_value.Number(strconv.FormatFloat(float64(typed), 'g', -1, 64))
	case Integer:
		return jsonInteger(int(typed))
	}
	// Invariant: the cases above are every type that declares isValue, which only this package can.
	panic(fmt.Sprintf("estree: a property value of type %T", value))
}
