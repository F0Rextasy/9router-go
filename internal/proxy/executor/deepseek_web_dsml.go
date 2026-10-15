package executor

import (
	json "encoding/json/v2"
	"fmt"
	"slices"
	"strings"
)

// DSML is the tool-call dialect the site models actually speak.
//
// The prompt asks for a single JSON object and a model follows it often enough to
// work — but these models have a native tool syntax they prefer, and when they
// reach for it the answer comes back as a DSML block instead. Passing that
// through as content is the worst outcome available: the client sees markup
// ending in `parameter>`, has no call to run, and concludes the model refused to
// answer. So the block is parsed here and returned as an ordinary call.
//
// The markers use a fullwidth vertical bar (U+FF5C), not an ASCII pipe, and the
// models pad them with spaces. Matching the substring between the bars rather
// than a fixed spelling is what makes both forms work.

// deepSeekDSMLInvokeOpen opens an invoke element.
const deepSeekDSMLInvokeOpen = "<｜｜DSML｜｜ invoke"

// deepSeekDSMLInvokeClose closes an invoke element.
const deepSeekDSMLInvokeClose = "<｜｜DSML｜｜ invoke>"

// deepSeekDSMLParameterOpen opens a parameter element.
const deepSeekDSMLParameterOpen = "<｜｜DSML｜｜ parameter"

// deepSeekDSMLParameterClose closes a parameter element.
const deepSeekDSMLParameterClose = "<｜｜DSML｜｜ parameter>"

// deepSeekDSMLCallsClose closes the block wrapping several invokes.
const deepSeekDSMLCallsClose = "<｜｜DSML｜｜ calls>"

// deepSeekExtractDSML recovers a tool call written in DSML.
//
// Only an invoke naming a tool the client declared is returned. A model that
// invents a tool name is passed through as content — the same conservative rule
// the JSON path follows, for the same reason: guessing produces an invocation the
// client then acts on.
func deepSeekExtractDSML(text string, tools []deepSeekToolSpec) (deepSeekToolRequest, bool) {
	for _, invoke := range deepSeekDSMLInvokes(text) {
		if call, ok := deepSeekParseDSMLInvoke(invoke, tools); ok {
			return call, true
		}
	}
	return deepSeekToolRequest{}, false
}

// deepSeekDSMLInvokes returns every invoke element in the answer, verbatim.
//
// The text is returned with its markers because the caller strips the call out of
// the answer to recover whatever the model wrote around it, and that only works if
// the removed text is exactly what was in the answer.
func deepSeekDSMLInvokes(text string) []string {
	var invokes []string
	rest := text
	for {
		start := strings.Index(rest, deepSeekDSMLInvokeOpen)
		if start < 0 {
			return invokes
		}
		opened := rest[start:]
		rest = rest[start+len(deepSeekDSMLInvokeOpen):]

		openEnd := strings.Index(rest, ">")
		if openEnd < 0 {
			return invokes
		}
		attributes := rest[:openEnd]
		rest = rest[openEnd+1:]

		bodyEnd := strings.Index(rest, deepSeekDSMLInvokeClose)
		if bodyEnd < 0 {
			return invokes
		}
		body := rest[:bodyEnd]
		invokes = append(invokes, opened+attributes+">"+body+deepSeekDSMLInvokeClose)
		rest = rest[bodyEnd+len(deepSeekDSMLInvokeClose):]
	}
}

// deepSeekParseDSMLInvoke turns one invoke element into a call.
func deepSeekParseDSMLInvoke(invoke string, tools []deepSeekToolSpec) (deepSeekToolRequest, bool) {
	attributes, body, found := strings.Cut(invoke, ">")
	if !found {
		return deepSeekToolRequest{}, false
	}

	name := deepSeekDSMLAttribute(attributes, "name")
	if name == "" || !deepSeekToolDeclared(name, tools) {
		return deepSeekToolRequest{}, false
	}

	return deepSeekToolRequest{
		Name:      name,
		Arguments: deepSeekDSMLArguments(body),
		Raw:       invoke,
	}, true
}

// deepSeekDSMLAttribute reads one quoted attribute value.
//
// The name is matched only where it is a whole attribute, so a `name=` nested in
// a longer attribute name cannot be mistaken for it.
func deepSeekDSMLAttribute(attributes, key string) string {
	marker := key + "="
	rest := attributes
	for {
		at := strings.Index(rest, marker)
		if at < 0 {
			return ""
		}
		// The preceding character decides whether this `name=` is an attribute of
		// its own or the tail of a longer one, so it is read before the slice moves.
		wholeAttribute := at == 0 || isDSMLAttrBreak(rest[at-1])
		rest = rest[at+len(marker):]
		if !wholeAttribute {
			continue
		}
		if rest == "" {
			return ""
		}
		quote := rest[0]
		if quote != '"' && quote != '\'' {
			return ""
		}
		value, _, ok := strings.Cut(rest[1:], string(quote))
		if !ok {
			return ""
		}
		return value
	}
}

func isDSMLAttrBreak(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// deepSeekDSMLArguments builds the argument object from the parameter elements.
//
// Order is not part of the format, so the properties are emitted sorted by name.
// That keeps the result byte-identical across identical turns, which matters
// because the arguments string is compared when deciding whether a call repeats.
func deepSeekDSMLArguments(body string) string {
	params := deepSeekDSMLParameters(body)

	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	slices.Sort(names)

	var b strings.Builder
	b.WriteString("{")
	for i, name := range names {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q:%s", name, deepSeekDSMLValue(params[name]))
	}
	b.WriteString("}")
	return b.String()
}

// deepSeekDSMLParameters returns every parameter element's body, keyed by name.
func deepSeekDSMLParameters(body string) map[string]string {
	params := make(map[string]string)
	rest := body
	for {
		start := strings.Index(rest, deepSeekDSMLParameterOpen)
		if start < 0 {
			return params
		}
		rest = rest[start+len(deepSeekDSMLParameterOpen):]

		openEnd := strings.Index(rest, ">")
		if openEnd < 0 {
			return params
		}
		attributes := rest[:openEnd]
		rest = rest[openEnd+1:]

		bodyEnd := strings.Index(rest, deepSeekDSMLParameterClose)
		if bodyEnd < 0 {
			return params
		}
		if name := deepSeekDSMLAttribute(attributes, "name"); name != "" {
			params[name] = rest[:bodyEnd]
		}
		rest = rest[bodyEnd+len(deepSeekDSMLParameterClose):]
	}
}

// deepSeekDSMLValue renders one parameter body as a JSON value.
//
// Every DSML value is a string; `string="true"` is an attribute describing the
// body, not a JSON type. Emitting the body verbatim as a string is what the
// format carries, and coercing it to a number or bool here would mean guessing
// the tool's schema from the model's prose.
func deepSeekDSMLValue(body string) string {
	encoded, err := json.Marshal(strings.TrimSpace(body))
	if err != nil {
		return `""`
	}
	return string(encoded)
}

// deepSeekStripDSML removes a DSML block from an answer.
//
// Used only when the block could not be turned into a call — an undeclared tool
// name, say. Leaving it in shows the user raw markup for something meant to be a
// tool call, which reads as corruption.
func deepSeekStripDSML(text string) string {
	start := strings.Index(text, "<｜｜DSML｜｜")
	if start < 0 {
		return text
	}
	// A block ends at the calls terminator, or runs to the end of the answer when
	// the model was cut off partway through writing one.
	if end := strings.Index(text[start:], deepSeekDSMLCallsClose); end > 0 {
		return strings.TrimSpace(text[:start] + text[start+end+len(deepSeekDSMLCallsClose):])
	}
	return strings.TrimSpace(text[:start])
}

// deepSeekDSMLInvokeCount reports how many invokes an answer carries, for the log.
func deepSeekDSMLInvokeCount(text string) int {
	return strings.Count(text, deepSeekDSMLInvokeOpen)
}
