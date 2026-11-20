package mapperkit

import (
	"fmt"
	"regexp"
	"strings"
)

// RefMode describes how much freedom a configuration field allows in its value.
type RefMode int

const (
	// ModeSingleRef allows exactly one placeholder and nothing around it, e.g.
	// "{{data[4]}}" or "{{data.content.phone.trim()}}". A plain literal without "{{" is
	// also accepted. Typical use: a field reference or the left side of a condition.
	ModeSingleRef RefMode = iota
	// ModeStringTemplate allows literals plus any number of placeholders, but no {{#if}}
	// blocks, e.g. "{{data[5]}}-{{data[6]}}" or "evt:{{flow_id}}:{{data.id}}". Typical use:
	// keys, deduplication keys, composite identifiers.
	ModeStringTemplate
	// ModeFullTemplate allows everything, including {{#if}} blocks. Typical use: a full
	// JSON output template.
	ModeFullTemplate
)

// SourceFormat is the message format of a data source; it decides which placeholder base
// (data[N] or data.path) is valid. FormatUnknown disables the base check (useful when the
// caller does not know the format yet).
type SourceFormat string

const (
	// FormatUnknown disables the source format check in ValidateRef.
	FormatUnknown SourceFormat = ""
	// FormatTSV is a tab-separated (positional) source: use {{data[N]}}.
	FormatTSV SourceFormat = "tsv"
	// FormatJSON is a JSON source: use {{data.path}}.
	FormatJSON SourceFormat = "json"
	// FormatXML is an XML source: use {{data.path}}.
	FormatXML SourceFormat = "xml"
	// FormatText is a plain-text source: data references are not supported.
	FormatText SourceFormat = "text"
)

// WithParsedXML parses RawXML once and returns a Context that can be reused across many
// Execute calls. Context is passed by value, so without this every Execute parses the XML
// again; a caller that evaluates several templates against the same event should call
// WithParsedXML once and pass the result to all of them.
func (ctx Context) WithParsedXML() Context {
	return ctx.withParsedXML()
}

// ─── Canonicalize ────────────────────────────────────────────────────────────

var (
	reCanonDigits    = regexp.MustCompile(`^[0-9]+$`)
	reCanonBracketed = regexp.MustCompile(`^\[([0-9]+)\]$`)
	reCanonDataIndex = regexp.MustCompile(`^data\[([0-9]+)\]$`)
)

// Canonicalize rewrites a short-hand field reference into its canonical placeholder form
// without changing its meaning. changed is true if the value was rewritten. Rules, in order:
//
//	"" or "none"         -> unchanged
//	contains "{{"        -> unchanged (already a template)
//	"4"                  -> "{{data[4]}}"
//	"[4]"                -> "{{data[4]}}"
//	"data[4]"            -> "{{data[4]}}"
//	anything else (path) -> "{{data.<path>}}"
//
// The last rule adds the "data." prefix mechanically, without heuristics: "data.phone" becomes
// "{{data.data.phone}}" on purpose. Whether the base fits the source is checked by ValidateRef,
// not by Canonicalize.
func Canonicalize(raw string) (canonical string, changed bool) {
	if raw == "" || raw == "none" {
		return raw, false
	}
	if strings.Contains(raw, "{{") {
		return raw, false
	}
	if reCanonDigits.MatchString(raw) {
		return "{{data[" + raw + "]}}", true
	}
	if m := reCanonBracketed.FindStringSubmatch(raw); m != nil {
		return "{{data[" + m[1] + "]}}", true
	}
	if m := reCanonDataIndex.FindStringSubmatch(raw); m != nil {
		return "{{data[" + m[1] + "]}}", true
	}
	return "{{data." + raw + "}}", true
}

// ─── ValidateRef ─────────────────────────────────────────────────────────────

// ValidateRef checks that s is allowed in a field with the given mode for a source of the
// given format. It returns nil for an empty string and for the keyword "none": deciding
// whether a field must be configured is up to the caller.
//
// Checks run in order and the first problem is returned:
//
//  1. s compiles (balanced braces, known bases and methods, method chain length, substr args);
//  2. ModeSingleRef: exactly one placeholder and no surrounding text (ErrNotSingleRef);
//  3. ModeStringTemplate: no {{#if}} blocks (ErrIfBlockNotAllowed);
//  4. the placeholder base matches format (ErrIndexBaseOnPathFormat, ErrPathBaseOnIndexFormat,
//     ErrDataBaseOnTextFormat); skipped for FormatUnknown;
//  5. no negative data[N] index (ErrInvalidDataIndex).
func ValidateRef(mode RefMode, format SourceFormat, s string) error {
	if s == "" || s == "none" {
		return nil
	}

	// 1. Compile: reuse the regular parser, which already catches unbalanced {{}}, unknown
	// bases and methods, chain length and broken substr arguments.
	parts, ok, err := compileForMode(mode, s)
	if err != nil || !ok {
		return err
	}

	if err := checkSingleRef(mode, s, parts); err != nil {
		return err
	}
	if err := checkIfBlockAllowed(mode, s); err != nil {
		return err
	}
	if err := checkFormatConsistency(format, s, parts); err != nil {
		return err
	}
	return checkNoNegativeIndex(s, parts)
}

// compileForMode runs the compile step of ValidateRef (step 1). ok=false means s compiled
// to an empty template (nil *CompiledString/*CompiledTemplate) — nothing left to validate.
func compileForMode(mode RefMode, s string) (parts []part, ok bool, err error) {
	if mode == ModeFullTemplate {
		ct, err := Compile(s)
		if err != nil || ct == nil {
			return nil, ct != nil, err
		}
		return ct.parts, true, nil
	}
	cs, err := CompileString(s)
	if err != nil || cs == nil {
		return nil, cs != nil, err
	}
	return cs.parts, true, nil
}

// checkSingleRef enforces step 2: in ModeSingleRef, if "{{" appears at all, s must be exactly
// one placeholder and nothing else. A string without "{{" (a plain literal) is legal — the
// mode only forbids mixing, not the literal itself (condition.right).
func checkSingleRef(mode RefMode, s string, parts []part) error {
	if mode != ModeSingleRef || !strings.Contains(s, "{{") {
		return nil
	}
	if len(parts) != 1 || parts[0].placeholder == nil || parts[0].ifBlock != nil {
		return fmt.Errorf("%w: %q: must be exactly one {{...}} reference without surrounding text", ErrNotSingleRef, s)
	}
	return nil
}

// checkIfBlockAllowed enforces step 3: ModeStringTemplate does not allow {{#if}} blocks.
func checkIfBlockAllowed(mode RefMode, s string) error {
	if mode == ModeStringTemplate && strings.Contains(s, "{{#if") {
		return fmt.Errorf("%w: %q: {{#if}} is not allowed in this field", ErrIfBlockNotAllowed, s)
	}
	return nil
}

// checkFormatConsistency enforces step 4: the placeholder base must match the source format,
// checked only when format is known. header["k"]/cookie["k"]/query["k"] are never checked
// here: their availability depends on the source type (HTTP), not the message format — that's
// the caller's call.
func checkFormatConsistency(format SourceFormat, s string, parts []part) error {
	if format == FormatUnknown {
		return nil
	}
	for _, ph := range collectPlaceholders(parts) {
		switch {
		case ph.kind == kindDataIndex && (format == FormatJSON || format == FormatXML):
			return fmt.Errorf("%w: %q: use {{data.<path>}} for a %s source", ErrIndexBaseOnPathFormat, s, format)
		case ph.kind == kindDataField && format == FormatTSV:
			return fmt.Errorf("%w: %q: use {{data[N]}} for a TSV source", ErrPathBaseOnIndexFormat, s)
		case (ph.kind == kindDataIndex || ph.kind == kindDataField) && format == FormatText:
			return fmt.Errorf("%w: %q: a text source does not support data references", ErrDataBaseOnTextFormat, s)
		}
	}
	return nil
}

// checkNoNegativeIndex enforces step 5: data[-1] compiles fine but silently resolves to "" at
// runtime — on write, that needs to be caught explicitly.
func checkNoNegativeIndex(s string, parts []part) error {
	for _, ph := range collectPlaceholders(parts) {
		if ph.kind == kindDataIndex && ph.index < 0 {
			return fmt.Errorf("%w: %q", ErrInvalidDataIndex, s)
		}
	}
	return nil
}

// collectPlaceholders walks already compiled parts (including {{#if}}/{{#else}} branches and
// the left side of their conditions) and returns all placeholders, without re-parsing the
// string.
func collectPlaceholders(parts []part) []*placeholder {
	var out []*placeholder
	for i := range parts {
		p := &parts[i]
		if p.placeholder != nil {
			out = append(out, p.placeholder)
		}
		if p.ifBlock == nil {
			continue
		}
		for _, br := range p.ifBlock.branches {
			if br.cond != nil && br.cond.left != nil {
				out = append(out, collectPlaceholders(br.cond.left.parts)...)
			}
			out = append(out, collectPlaceholders(br.parts)...)
		}
		if p.ifBlock.elseParts != nil {
			out = append(out, collectPlaceholders(p.ifBlock.elseParts)...)
		}
	}
	return out
}
