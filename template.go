package mapperkit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// normalizeGJSONPath converts field[N] array notation to field.N, which gjson requires.
func normalizeGJSONPath(path string) string {
	if !strings.ContainsRune(path, '[') {
		return path
	}
	var b strings.Builder
	b.Grow(len(path))
	i := 0
	for i < len(path) {
		if path[i] == '[' {
			j := strings.IndexByte(path[i:], ']')
			if j > 0 {
				inner := path[i+1 : i+j]
				if _, err := strconv.Atoi(inner); err == nil {
					b.WriteByte('.')
					b.WriteString(inner)
					i += j + 1
					continue
				}
			}
		}
		b.WriteByte(path[i])
		i++
	}
	return b.String()
}

// Context is the input a template is executed against. Set only the fields your
// templates use; a placeholder whose source is empty resolves to "".
type Context struct {
	// FlowId is the value of the {{flow_id}} placeholder (any caller-defined identifier
	// of the pipeline, stream or tenant that produced the event).
	FlowId string //nolint:revive // kept for API compatibility
	// RawFields are the positional fields of a TSV/CSV-like event, read by {{data[N]}}.
	RawFields []string
	// RawJSON is a JSON event, read by {{data.path}} (gjson syntax; a[0] is also accepted).
	RawJSON []byte
	// RawXML is an XML event, read by {{data.path}} when RawJSON is empty.
	RawXML []byte
	// Headers are read by {{header["name"]}}.
	Headers map[string]string
	// Cookies are read by {{cookie["name"]}}.
	Cookies map[string]string
	// QueryParams are read by {{query["name"]}}.
	QueryParams map[string]string

	parsedXML *xmlNode
}

// withParsedXML pre-parses RawXML once and returns an updated copy of ctx.
// Both Execute methods call this before resolving placeholders to avoid repeated parsing.
func (ctx Context) withParsedXML() Context {
	if len(ctx.RawXML) > 0 && ctx.parsedXML == nil {
		if tree, err := parseXMLToTree(ctx.RawXML); err == nil {
			ctx.parsedXML = tree
		}
	}
	return ctx
}

// CompiledTemplate is a whole output template (usually JSON) parsed as a text template.
// Placeholders {{...}} are resolved and the result should be valid JSON.
// This supports unquoted placeholders for int/bool values.
type CompiledTemplate struct {
	parts []part
}

// CompiledString is a compiled single string template (for key templates etc.).
type CompiledString struct {
	parts []part
}

type part struct {
	literal      string
	placeholder  *placeholder
	ifBlock      *ifBlock
	inJSONString bool // placeholder sits inside a JSON string literal → escape on execute
}

type placeholderKind int

const (
	kindDataIndex placeholderKind = iota
	kindDataField
	kindFlowId
	kindTimestamp
	kindUUID
	kindHeader
	kindCookie
	kindQuery
)

type placeholder struct {
	kind    placeholderKind
	index   int    // for data[N]
	field   string // for data.field or data.a.b.c
	key     string // for header["key"], cookie["key"], query["key"]
	methods []methodCall
}

type methodCall struct {
	name string
	arg  string
	arg2 string

	// timeCache holds this method call's compiled date-layout hint — only set for
	// "format"/"toUnix" (see splitTrailingMethod), nil for everything else. It's a pointer
	// rather than an embedded atomic.Int32 so that reordering methodCall values (append,
	// slices.Reverse in extractMethods) copies a pointer, not a no-copy atomic — the target
	// timeParser itself never moves once allocated.
	timeCache *timeParser
}

// CompileString parses a single string template with {{...}} placeholders.
// Returns nil if template is empty.
func CompileString(template string) (*CompiledString, error) {
	if template == "" {
		return nil, nil
	}

	parts, err := parseParts(template)
	if err != nil {
		return nil, err
	}

	return &CompiledString{parts: parts}, nil
}

// Execute resolves the compiled string template with the given context.
func (t *CompiledString) Execute(ctx Context) (string, error) {
	return resolveParts(t.parts, ctx.withParsedXML())
}

// Compile parses an output template as a text template with {{...}} placeholders.
// The template is NOT parsed as JSON — placeholders can appear unquoted for int/bool values.
// Returns nil if template is empty (passthrough mode).
func Compile(templateStr string) (*CompiledTemplate, error) {
	if templateStr == "" {
		return nil, nil
	}

	parts, err := parseParts(templateStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCompile, err)
	}

	stripUnquotedPlaceholderQuotes(parts)
	markJSONStringContext(parts, false)

	return &CompiledTemplate{parts: parts}, nil
}

// markJSONStringContext walks the compiled parts in template order, tracking whether the
// cursor is inside a JSON string literal (odd number of unescaped double quotes seen so
// far), and marks every placeholder that lands inside one. Returns the parity after the
// last part so {{#if}} branches can be scanned from the parity at block entry.
//
// Must run after stripUnquotedPlaceholderQuotes: that pass removes the surrounding quotes
// around toInt/toUnix/hexToInt/toBool placeholders, and only once those quotes are gone do
// such placeholders honestly sit outside a string — escaping them would be wrong (it would
// re-quote a JSON number/boolean into a string).
func markJSONStringContext(parts []part, inString bool) bool { //nolint:unparam // parity is returned for recursive callers
	for i := range parts {
		p := &parts[i]
		switch {
		case p.ifBlock != nil:
			// Every branch is scanned from the parity at block entry; the parity after
			// the block equals the parity before it (template branches are required to
			// be balanced), so the per-branch return value is intentionally ignored.
			for j := range p.ifBlock.branches {
				markJSONStringContext(p.ifBlock.branches[j].parts, inString)
			}
			if p.ifBlock.elseParts != nil {
				markJSONStringContext(p.ifBlock.elseParts, inString)
			}
		case p.placeholder != nil:
			// The placeholder's value is substituted INSIDE the string, so it doesn't
			// itself flip the parity.
			p.inJSONString = inString
		default:
			inString = scanQuoteParity(p.literal, inString)
		}
	}
	return inString
}

// scanQuoteParity walks literal byte-by-byte and toggles inString on every unescaped '"' —
// a backslash escapes the next byte, so a template-literal `\"` doesn't flip parity — and
// returns the resulting parity.
func scanQuoteParity(literal string, inString bool) bool {
	i := 0
	for i < len(literal) {
		switch literal[i] {
		case '\\':
			i += 2
			continue
		case '"':
			inString = !inString
		}
		i++
	}
	return inString
}

// stripUnquotedPlaceholderQuotes removes the surrounding JSON string quotes from
// placeholders whose LAST method in the chain produces a non-string JSON value (an
// integer or a boolean — see isUnquotedMethod) when the placeholder is the sole content
// between those quotes. This allows them to render as bare JSON numbers/booleans instead
// of strings. Also recurses into {{#if}} block branches.
//
// Example: "{{data[0].toUnix()}}" → {{data[0].toUnix()}}  (unquoted → JSON number)
// Chain:   "{{data[0].substr(5).hexToInt()}}" → unquoted   (last method decides, not first)
// Mixed:   "{{data[0].toUnix()}} suffix" → left as-is      (part of a string)
func stripUnquotedPlaceholderQuotes(parts []part) {
	for i := range parts {
		p := &parts[i]

		if p.ifBlock != nil {
			for j := range p.ifBlock.branches {
				stripUnquotedPlaceholderQuotes(p.ifBlock.branches[j].parts)
			}
			if p.ifBlock.elseParts != nil {
				stripUnquotedPlaceholderQuotes(p.ifBlock.elseParts)
			}
			continue
		}

		if p.placeholder == nil || !isUnquotedMethod(p.placeholder.methods) {
			continue
		}
		if i == 0 || i == len(parts)-1 {
			continue
		}
		prev := &parts[i-1]
		next := &parts[i+1]
		if prev.placeholder != nil || next.placeholder != nil {
			continue
		}
		if !strings.HasSuffix(prev.literal, `"`) || !strings.HasPrefix(next.literal, `"`) {
			continue
		}
		prev.literal = prev.literal[:len(prev.literal)-1]
		next.literal = next.literal[1:]
	}
}

// isUnquotedMethod reports whether the LAST method in the chain produces a value that
// should render unquoted in a JSON template — an integer (toInt, toUnix, hexToInt) or a
// boolean (toBool). Earlier methods in the chain don't matter: only the last one decides
// the final type, e.g. .substr(5).hexToInt() → number, .hexToInt().format('...') → string.
func isUnquotedMethod(methods []methodCall) bool {
	if len(methods) == 0 {
		return false
	}
	switch methods[len(methods)-1].name {
	case "toInt", "toUnix", "hexToInt", "toBool":
		return true
	default:
		return false
	}
}

// Execute resolves all placeholders and returns the result as bytes.
func (t *CompiledTemplate) Execute(ctx Context) ([]byte, error) {
	result, err := resolveParts(t.parts, ctx.withParsedXML())
	if err != nil {
		return nil, err
	}

	return []byte(result), nil
}

func parseParts(template string) ([]part, error) {
	tokens, err := tokenizeTemplate(template)
	if err != nil {
		return nil, err
	}

	parts, finalPos, err := buildPartsFromTokens(tokens, 0)
	if err != nil {
		return nil, err
	}

	if finalPos < len(tokens) {
		switch tokens[finalPos].kind {
		case tokKindElse, tokKindElseIf:
			return nil, fmt.Errorf("%w: {{#else}} without {{#if}}", ErrIfBlockSyntax)
		default:
			return nil, fmt.Errorf("%w: {{/if}} without {{#if}}", ErrIfBlockSyntax)
		}
	}

	return parts, nil
}

// maxMethodChainLen caps the number of chained methods on one placeholder — a guard
// against pathological templates, not a real-world need (the deepest known chain,
// .substr().hexToInt(), is 2).
const maxMethodChainLen = 5

func parsePlaceholder(expr string) (*placeholder, error) {
	baseExpr, methods, err := extractMethods(expr)
	if err != nil {
		return nil, err
	}
	if len(methods) > maxMethodChainLen {
		return nil, fmt.Errorf("%w: %d methods (max %d): %q", ErrTooManyMethods, len(methods), maxMethodChainLen, expr)
	}

	ph, err := parseBasePlaceholder(baseExpr)
	if err != nil {
		return nil, err
	}
	ph.methods = methods

	return ph, nil
}

// extractMethods peels chained ".identifier(args)" calls off the tail of expr, right to
// left, quote-aware (an argument like .replace('a.b', 'x,y') must not be mistaken for two
// methods or a base-placeholder dot). Returns the remaining base expression and the
// methods in left-to-right application order — "data[0].substr(5).hexToInt()" yields base
// "data[0]" and methods [substr, hexToInt], applied in that order.
//
// XML predicates (param[key=SERV_ID].value) and gjson paths (a.b[0].c) are never mistaken
// for method calls: they don't end in ")", so the loop below exits on the first attempt.
func extractMethods(expr string) (string, []methodCall, error) {
	var methods []methodCall
	rest := expr

	for {
		newRest, m, ok, err := splitTrailingMethod(rest)
		if err != nil {
			return "", nil, err
		}
		if !ok {
			break
		}
		methods = append(methods, m)
		rest = newRest
	}

	slices.Reverse(methods)
	return rest, methods, nil
}

// splitTrailingMethod extracts one ".name(args)" call from the end of expr, if present.
// ok is false (not an error) when expr simply doesn't end in a method call — e.g. it's a
// plain field path, or the base placeholder has been fully consumed.
func splitTrailingMethod(expr string) (rest string, m methodCall, ok bool, err error) {
	if !strings.HasSuffix(expr, ")") {
		return "", methodCall{}, false, nil
	}
	closeIdx := len(expr) - 1

	openIdx := findMatchingOpenParen(expr, closeIdx)
	if openIdx == -1 {
		return "", methodCall{}, false, nil
	}

	nameStart := openIdx
	for nameStart > 0 && isIdentByte(expr[nameStart-1]) {
		nameStart--
	}
	if nameStart == openIdx || nameStart == 0 || expr[nameStart-1] != '.' {
		// no identifier before "(", or no "." before the identifier — not a method call.
		return "", methodCall{}, false, nil
	}
	name := expr[nameStart:openIdx]
	if !isLetterByte(name[0]) {
		return "", methodCall{}, false, nil
	}

	rawArgs := expr[openIdx+1 : closeIdx]
	arg, arg2 := parseMethodArgs(rawArgs)
	if verr := validateMethodArgs(name, arg, arg2); verr != nil {
		return "", methodCall{}, false, verr
	}

	mc := methodCall{name: name, arg: arg, arg2: arg2}
	if name == "format" || name == "toUnix" {
		// One cache per compiled placeholder, not a package-global: a process can serve
		// several sources (API validates templates from many at once), and different
		// placeholders in the same template can read fields of different date shapes.
		mc.timeCache = newTimeParser()
	}

	return expr[:nameStart-1], mc, true, nil
}

// findMatchingOpenParen scans expr backward from closeIdx (expr[closeIdx] == ')') and
// returns the index of its matching "(", or -1. Quote-aware: parens inside '...' segments
// (e.g. .replace('(', ')')) don't count — toggling on every ' is order-independent, so
// scanning backward is as correct as forward as long as closeIdx itself starts outside a
// quote, which it always does here (a template can't end a placeholder mid-string).
func findMatchingOpenParen(expr string, closeIdx int) int {
	depth := 0
	inQuote := false
	for i := closeIdx; i >= 0; i-- {
		switch expr[i] {
		case '\'':
			inQuote = !inQuote
		case ')':
			if !inQuote {
				depth++
			}
		case '(':
			if !inQuote {
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}
	return -1
}

func isIdentByte(c byte) bool {
	return isLetterByte(c) || (c >= '0' && c <= '9')
}

func isLetterByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// validateMethodArgs runs compile-time argument checks for methods whose arguments must be
// well-formed before Execute ever runs — currently just substr's integer offsets. Other
// methods either take no arguments or tolerate any string (e.g. .format(layout)) and are
// validated, if at all, at render time by their apply* function.
func validateMethodArgs(name, arg, arg2 string) error {
	if name != "substr" {
		return nil
	}
	if _, err := strconv.Atoi(arg); err != nil {
		return fmt.Errorf("%w: substr: from %q: %w", ErrInvalidMethodArg, arg, err)
	}
	if arg2 != "" {
		if _, err := strconv.Atoi(arg2); err != nil {
			return fmt.Errorf("%w: substr: len %q: %w", ErrInvalidMethodArg, arg2, err)
		}
	}
	return nil
}

// unquote strips a pair of surrounding single quotes from s and interprets
// backslash escape sequences within the result.
// Double quotes are intentionally NOT recognized: they would clash with the JSON
// quotes around the placeholder in a JSON template.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return interpretEscapes(s[1 : len(s)-1])
	}
	return s
}

// interpretEscapes replaces backslash escape sequences in s:
//
//	\uXXXX — Unicode code point (exactly 4 hex digits)
//	\\      — literal backslash
//	\n      — newline
//	\t      — tab
//	\r      — carriage return
func interpretEscapes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		switch s[i+1] {
		case 'u':
			if i+5 < len(s) {
				n, err := strconv.ParseInt(s[i+2:i+6], 16, 32)
				if err == nil {
					b.WriteRune(rune(n))
					i += 6
					continue
				}
			}
			b.WriteByte(s[i])
			i++
		case '\\':
			b.WriteByte('\\')
			i += 2
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// parseMethodArgs splits the raw argument string into up to two arguments.
// The split is quote-aware: commas inside single-quoted segments are part of the
// argument, not separators. Surrounding single quotes on each argument are stripped.
// Returns (arg1, arg2); arg2 is empty for single-argument methods.
func parseMethodArgs(raw string) (string, string) {
	idx := indexUnquotedComma(raw)
	if idx == -1 {
		return unquote(strings.TrimSpace(raw)), ""
	}
	return unquote(strings.TrimSpace(raw[:idx])), unquote(strings.TrimSpace(raw[idx+1:]))
}

// indexUnquotedComma returns the index of the first comma in s that is not
// inside a single-quoted segment. Returns -1 if no such comma exists.
func indexUnquotedComma(s string) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				return i
			}
		}
	}
	return -1
}

func parseBasePlaceholder(expr string) (*placeholder, error) {
	switch {
	case expr == "flow_id":
		return &placeholder{kind: kindFlowId}, nil
	case expr == "timestamp":
		return &placeholder{kind: kindTimestamp}, nil
	case expr == "uuid":
		return &placeholder{kind: kindUUID}, nil
	case strings.HasPrefix(expr, "data["):
		indexStr := strings.TrimPrefix(expr, "data[")
		indexStr = strings.TrimSuffix(indexStr, "]")
		idx, err := strconv.Atoi(indexStr)
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrInvalidDataIndex, expr, err)
		}
		return &placeholder{kind: kindDataIndex, index: idx}, nil
	case strings.HasPrefix(expr, "data."):
		field := strings.TrimPrefix(expr, "data.")
		return &placeholder{kind: kindDataField, field: field}, nil
	case strings.HasPrefix(expr, `header["`) && strings.HasSuffix(expr, `"]`):
		key := expr[len(`header["`) : len(expr)-len(`"]`)]
		return &placeholder{kind: kindHeader, key: key}, nil
	case strings.HasPrefix(expr, `cookie["`) && strings.HasSuffix(expr, `"]`):
		key := expr[len(`cookie["`) : len(expr)-len(`"]`)]
		return &placeholder{kind: kindCookie, key: key}, nil
	case strings.HasPrefix(expr, `query["`) && strings.HasSuffix(expr, `"]`):
		key := expr[len(`query["`) : len(expr)-len(`"]`)]
		return &placeholder{kind: kindQuery, key: key}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownPlaceholder, expr)
	}
}

func resolveParts(parts []part, ctx Context) (string, error) {
	if len(parts) == 1 && parts[0].ifBlock == nil {
		if parts[0].placeholder == nil {
			return parts[0].literal, nil
		}
		// Fast-path for "one placeholder, no surrounding literal" — e.g. a bare
		// {{data[4]}} source position. Skips the strings.Builder entirely: for
		// TSV this makes position resolution as allocation-free as the old
		// fields[idx] lookup.
		return resolvePlaceholder(parts[0].placeholder, ctx)
	}

	var b strings.Builder
	for _, p := range parts {
		switch {
		case p.ifBlock != nil:
			val, err := resolveIfBlock(p.ifBlock, ctx)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
		case p.placeholder != nil:
			val, err := resolvePlaceholder(p.placeholder, ctx)
			if err != nil {
				return "", err
			}
			if p.inJSONString {
				writeJSONEscaped(&b, val)
			} else {
				b.WriteString(val)
			}
		default:
			b.WriteString(p.literal)
		}
	}

	return b.String(), nil
}

func resolvePlaceholder(ph *placeholder, ctx Context) (string, error) {
	var val string

	switch ph.kind {
	case kindFlowId:
		val = ctx.FlowId
	case kindTimestamp:
		val = strconv.FormatInt(time.Now().Unix(), 10)
	case kindUUID:
		val = uuid.NewString()
	case kindDataIndex:
		if ph.index < 0 || ph.index >= len(ctx.RawFields) {
			val = ""
		} else {
			val = ctx.RawFields[ph.index]
		}
	case kindDataField:
		if len(ctx.RawJSON) > 0 {
			val = resolveJSONField(ctx.RawJSON, ph.field)
		} else if ctx.parsedXML != nil {
			val = navigateXMLTree(ctx.parsedXML, ph.field)
		}
	case kindHeader:
		val = ctx.Headers[ph.key]
	case kindCookie:
		val = ctx.Cookies[ph.key]
	case kindQuery:
		val = ctx.QueryParams[ph.key]
	default:
		return "", fmt.Errorf("%w: kind %d", ErrResolve, ph.kind)
	}

	for i := range ph.methods {
		var err error
		val, err = applyMethod(val, &ph.methods[i])
		if err != nil {
			return "", err
		}
	}

	return val, nil
}

// applyMethod applies one method call to the resolved value. Methods chain: the result of
// one call is the input to the next (see resolvePlaceholder), so e.g.
// .substr(5).hexToInt() first slices, then parses the slice as hex.
//
// Supported methods:
//
//	.format("2006-01-02")              — parse date and reformat with Go time layout
//	.toLower()                         — strings.ToLower
//	.toUpper()                         — strings.ToUpper
//	.trim()                            — strings.TrimSpace
//	.toUnix()                          — parse date → Unix timestamp string
//	.toInt()                           — parse string → integer string (strips non-numeric)
//	.unixFormat(2006-01-02 15:04:05)   — Unix timestamp → formatted datetime string
//	.replace('old', 'new')             — strings.ReplaceAll; args MUST be in single quotes
//	.substr(from)                      — substring from a rune index to the end
//	.substr(from, len)                 — substring, from a rune index, up to len runes
//	.hexToInt()                        — parse hex string (optional 0x/0X prefix) → decimal string
//	.host()                            — strip scheme/path/port from a URL-ish string
//	.toBool()                          — parse truthy string/number → "true"/"false"
func applyMethod(val string, m *methodCall) (string, error) {
	switch m.name {
	case "format":
		return applyFormat(val, m.arg, m.timeCache)
	case "toLower":
		return strings.ToLower(val), nil
	case "toUpper":
		return strings.ToUpper(val), nil
	case "trim":
		return strings.TrimSpace(val), nil
	case "toUnix":
		return applyToUnix(val, m.timeCache), nil
	case "toInt":
		return applyToInt(val), nil
	case "unixFormat":
		return applyUnixFormat(val, m.arg), nil
	case "replace":
		return strings.ReplaceAll(val, m.arg, m.arg2), nil
	case "substr":
		return applySubstr(val, m.arg, m.arg2)
	case "hexToInt":
		return applyHexToInt(val), nil
	case "host":
		return applyHost(val), nil
	case "toBool":
		return applyToBool(val), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownMethod, m.name)
	}
}

// applyFormat parses the value as a date and reformats it using Go time layout.
func applyFormat(val, layout string, tc *timeParser) (string, error) {
	t, err := parseTimeCached(val, tc)
	if err != nil {
		return val, nil
	}

	return t.Format(layout), nil
}

// applyToUnix parses the value as a date and returns Unix timestamp.
func applyToUnix(val string, tc *timeParser) string {
	t, err := parseTimeCached(val, tc)
	if err != nil {
		return val
	}

	return strconv.FormatInt(t.Unix(), 10)
}

// applyToInt strips non-numeric characters and returns the integer string.
func applyToInt(val string) string {
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		// try to parse as float and truncate
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return "0"
		}
		return strconv.FormatInt(int64(f), 10)
	}

	return strconv.FormatInt(n, 10)
}

// applySubstr slices val by rune index [from, from+len). from and len are already validated
// as integers at compile time (validateMethodArgs) — Atoi here cannot fail.
// A negative from counts from the end. Out-of-range from/len clip to the string's bounds
// instead of erroring; len <= 0 (when given) yields "".
func applySubstr(val, fromStr, lenStr string) (string, error) {
	from, _ := strconv.Atoi(fromStr)

	runes := []rune(val)
	n := len(runes)

	start := from
	if start < 0 {
		start += n
		if start < 0 {
			start = 0
		}
	}
	if start > n {
		start = n
	}

	end := n
	if lenStr != "" {
		length, _ := strconv.Atoi(lenStr)
		if length <= 0 {
			return "", nil
		}
		end = start + length
		if end > n {
			end = n
		}
	}

	if start >= end {
		return "", nil
	}
	return string(runes[start:end]), nil
}

// applyHexToInt parses val as a hex integer (optional 0x/0X prefix) and returns its decimal
// string form. Never errors: an unparsable value yields "0", matching applyToInt's fallback.
func applyHexToInt(val string) string {
	s := strings.TrimSpace(val)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")

	n, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return "0"
	}
	return strconv.FormatInt(n, 10)
}

// applyHost strips scheme, path and port from a URL-ish string with plain string
// operations rather than net/url, which would error on the malformed strings this sees in practice:
//  1. "://" present → drop everything up to and including it (scheme)
//  2. "/" present → drop it and everything after (path)
//  3. last ":" present → drop it and everything after (port)
//
// IPv6 literals ("[::1]:80") break step 3; this simple behavior is kept deliberately
// rather than special-cased.
func applyHost(val string) string {
	if idx := strings.Index(val, "://"); idx != -1 {
		val = val[idx+3:]
	}
	if idx := strings.IndexByte(val, '/'); idx != -1 {
		val = val[:idx]
	}
	if idx := strings.LastIndexByte(val, ':'); idx != -1 {
		val = val[:idx]
	}
	return val
}

// applyToBool converts a string or numeric representation to a JSON-boolean literal
// ("true"/"false"): source systems encode flags inconsistently ("1", "TRUE", "0.0", ...)
// but consumers expect a real boolean, not a passthrough string. Never errors — anything
// that isn't recognizably true parses to "false", same fail-safe spirit as applyToInt's "0".
//
//  1. case-insensitive "true"/"false" match directly
//  2. else parse as a number (int, then float): >= 1 is true, everything else is false
//  3. else "false"
func applyToBool(val string) string {
	trimmed := strings.TrimSpace(val)
	switch strings.ToLower(trimmed) {
	case "true":
		return "true"
	case "false":
		return "false"
	}

	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return strconv.FormatBool(n >= 1)
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return strconv.FormatBool(f >= 1)
	}

	return "false"
}

// applyUnixFormat parses the value as a Unix timestamp and formats it with the given Go layout.
// Falls back to the original value if parsing fails. Defaults to "2006-01-02 15:04:05" if no layout given.
func applyUnixFormat(val, layout string) string {
	if layout == "" {
		layout = "2006-01-02 15:04:05"
	}

	n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	if err != nil {
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return val
		}
		n = int64(f)
	}

	return time.Unix(n, 0).UTC().Format(layout)
}

// common date formats to try when parsing dates
var dateFormats = []string{
	"2006-01-02T15:04:05Z07:00", // RFC3339
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"02.01.2006 15:04:05",
	"02.01.2006",
	"01/02/2006 15:04:05",
	"01/02/2006",
	time.RFC1123,
	time.RFC822,
}

// timeParser caches which dateFormats index last parsed successfully for one compiled
// .format()/.toUnix() call, so a homogeneous stream of values (e.g. a source whose dates are
// always "2006-01-02 15:04:05") skips straight to the layout that's going to match instead of
// scanning dateFormats from the top on every event. It lives on the compiled methodCall, not
// package-global — see the comment at its allocation in splitTrailingMethod.
//
// last is a hint, not an invariant: concurrent Execute calls sharing one CompiledTemplate can
// race the Load/Store below, but the worst case is just a stale hint, which parseTime treats
// as a miss and falls back to the ordinary scan. No lock needed.
type timeParser struct {
	last atomic.Int32 // dateFormats index that matched last time; -1 = not known yet
}

func newTimeParser() *timeParser {
	tp := &timeParser{}
	tp.last.Store(-1)
	return tp
}

// parseTimeCached is parseTime with tc's cached layout hint, updating the cache when the
// matched layout differs from the hint (including the miss/unix-timestamp/no-match cases,
// which all report -1). tc is nil-safe: a nil cache just means "no hint", every call.
func parseTimeCached(val string, tc *timeParser) (time.Time, error) {
	hint := int32(-1)
	if tc != nil {
		hint = tc.last.Load()
	}

	t, matched, err := parseTime(val, hint)

	if tc != nil && matched != hint {
		tc.last.Store(matched)
	}

	return t, err
}

// parseTime parses val against dateFormats. hint is a dateFormats index to try first (-1 for
// "no hint"); on a hint hit it returns immediately without touching the rest of dateFormats.
// On a miss — hint wrong, or none given — it falls back to the plain left-to-right scan,
// identical to the pre-cache behavior, so which layout wins on an ambiguous value (there are
// none among dateFormats today) and the error message are unaffected by the hint.
//
// Returns the dateFormats index that matched, or -1 for the unix-timestamp fallback and for
// no match at all, so the caller can update its hint.
func parseTime(val string, hint int32) (time.Time, int32, error) {
	if hint >= 0 && int(hint) < len(dateFormats) {
		if t, err := time.ParseInLocation(dateFormats[hint], val, time.Local); err == nil {
			return t, hint, nil
		}
	}

	for i, layout := range dateFormats {
		t, err := time.ParseInLocation(layout, val, time.Local)
		if err == nil {
			return t, int32(i), nil
		}
	}

	// try unix timestamp
	if n, err := strconv.ParseInt(val, 10, 64); err == nil {
		return time.Unix(n, 0), -1, nil
	}

	return time.Time{}, -1, fmt.Errorf("%w: %q", ErrParseDate, val)
}

// hexDigits is the lowercase hex alphabet used by writeJSONEscaped's \u00XX fallback, to
// match encoding/json's own escaping of control characters below 0x20.
const hexDigits = "0123456789abcdef"

// needsJSONEscape reports whether s contains any byte that must be escaped inside a JSON
// string literal. Hot path: the overwhelming majority of values need nothing, and then
// writeJSONEscaped is a plain WriteString with zero extra allocations.
func needsJSONEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '"' || c == '\\' || c < 0x20 {
			return true
		}
	}
	return false
}

// writeJSONEscaped writes s into b escaped for a JSON string literal (without the
// surrounding quotes). `<`, `>`, `&`, `/` and U+2028/U+2029 are deliberately left alone —
// unlike encoding/json, this isn't rendering into HTML, and the value must reach the
// consumer byte-for-byte after their json.Unmarshal.
func writeJSONEscaped(b *strings.Builder, s string) {
	if !needsJSONEscape(s) {
		b.WriteString(s)
		return
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xf])
				continue
			}
			b.WriteByte(c)
		}
	}
}

func resolveJSONField(data []byte, path string) string {
	if len(data) == 0 {
		return ""
	}

	res := gjson.GetBytes(data, normalizeGJSONPath(path))
	if !res.Exists() {
		return ""
	}

	return res.String()
}
