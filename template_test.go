package mapperkit

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// BenchmarkCompiledString_SinglePlaceholder exercises the resolveParts fast-path for
// "one placeholder, no surrounding literal" — the typical shape of a field reference.
// TSV context with 20 fields. Must report 0 allocs/op.
func BenchmarkCompiledString_SinglePlaceholder(b *testing.B) {
	tmpl, err := CompileString("{{data[4]}}")
	if err != nil {
		b.Fatalf("CompileString: %v", err)
	}
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "field-value"
	}
	ctx := Context{RawFields: fields}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tmpl.Execute(ctx); err != nil {
			b.Fatalf("Execute: %v", err)
		}
	}
}

var testXML = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ns2:notifyRequest xmlns:ns2="http://example.com/schemas/notify">
    <ns2:msgTypeId>-10002</ns2:msgTypeId>
    <ns2:creDate>2026-05-25T15:07:39.867+05:00</ns2:creDate>
    <ns2:clientId>10042</ns2:clientId>
    <ns2:phone>15550001234</ns2:phone>
    <ns2:eventContext>
        <ns2:param>
            <ns2:key>SERV_ID</ns2:key>
            <ns2:value>1046</ns2:value>
        </ns2:param>
        <ns2:param>
            <ns2:key>SG_ID</ns2:key>
            <ns2:value />
        </ns2:param>
        <ns2:param>
            <ns2:key>STAT_ID</ns2:key>
            <ns2:value>2</ns2:value>
        </ns2:param>
    </ns2:eventContext>
</ns2:notifyRequest>`)

func TestXMLMapping(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "top-level field with root",
			template: `{"phone":"{{data.notifyRequest.phone}}"}`,
			want:     `{"phone":"15550001234"}`,
		},
		{
			name:     "top-level field without root",
			template: `{"phone":"{{data.phone}}"}`,
			want:     `{"phone":"15550001234"}`,
		},
		{
			name:     "nested field",
			template: `{"clientId":"{{data.clientId}}"}`,
			want:     `{"clientId":"10042"}`,
		},
		{
			name:     "array first element key",
			template: `{"key":"{{data.eventContext.param[0].key}}"}`,
			want:     `{"key":"SERV_ID"}`,
		},
		{
			name:     "array first element value",
			template: `{"val":"{{data.eventContext.param[0].value}}"}`,
			want:     `{"val":"1046"}`,
		},
		{
			name:     "array third element value",
			template: `{"val":"{{data.eventContext.param[2].value}}"}`,
			want:     `{"val":"2"}`,
		},
		{
			name:     "empty element returns empty string",
			template: `{"val":"{{data.eventContext.param[1].value}}"}`,
			want:     `{"val":""}`,
		},
		{
			name:     "out-of-bounds index returns empty string",
			template: `{"val":"{{data.eventContext.param[99].value}}"}`,
			want:     `{"val":""}`,
		},
		{
			name:     "missing element returns empty string",
			template: `{"val":"{{data.nonexistent}}"}`,
			want:     `{"val":""}`,
		},
		{
			name:     "method on xml field",
			template: `{"phone":"{{data.phone.toLower()}}"}`,
			want:     `{"phone":"15550001234"}`,
		},
		{
			name:     "namespace stripping — ns2 prefix invisible",
			template: `{"date":"{{data.creDate}}"}`,
			want:     `{"date":"2026-05-25T15:07:39.867+05:00"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(tc.template)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := tmpl.Execute(Context{RawXML: testXML})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestXMLAttributeMapping(t *testing.T) {
	data := []byte(`<root><item id="42" name="test">hello</item></root>`)

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "attribute access via @",
			template: `{"id":"{{data.item.@id}}"}`,
			want:     `{"id":"42"}`,
		},
		{
			name:     "text content alongside attribute",
			template: `{"text":"{{data.item}}","name":"{{data.item.@name}}"}`,
			want:     `{"text":"hello","name":"test"}`,
		},
		{
			name:     "missing attribute returns empty string",
			template: `{"x":"{{data.item.@missing}}"}`,
			want:     `{"x":""}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(tc.template)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := tmpl.Execute(Context{RawXML: data})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestXMLPredicateMapping(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "predicate: find param by key child",
			template: `{"val":"{{data.eventContext.param[key=SERV_ID].value}}"}`,
			want:     `{"val":"1046"}`,
		},
		{
			name:     "predicate: different key in same array",
			template: `{"val":"{{data.eventContext.param[key=STAT_ID].value}}"}`,
			want:     `{"val":"2"}`,
		},
		{
			name:     "predicate: empty value element",
			template: `{"val":"{{data.eventContext.param[key=SG_ID].value}}"}`,
			want:     `{"val":""}`,
		},
		{
			name:     "predicate: non-existent key returns empty",
			template: `{"val":"{{data.eventContext.param[key=NO_SUCH_KEY].value}}"}`,
			want:     `{"val":""}`,
		},
		{
			name:     "predicate: read key child itself",
			template: `{"key":"{{data.eventContext.param[key=STAT_ID].key}}"}`,
			want:     `{"key":"STAT_ID"}`,
		},
		{
			name:     "predicate: with method",
			template: `{"val":"{{data.eventContext.param[key=SERV_ID].value.toInt()}}"}`,
			want:     `{"val":1046}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(tc.template)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := tmpl.Execute(Context{RawXML: testXML})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeGJSONPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"field", "field"},
		{"field.sub", "field.sub"},
		{"field[0]", "field.0"},
		{"field[0].sub", "field.0.sub"},
		{"a.b[2].c[1].d", "a.b.2.c.1.d"},
		{"field[abc]", "field[abc]"}, // non-numeric: unchanged
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := normalizeGJSONPath(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeGJSONPath(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseMethodArgs(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantArg  string
		wantArg2 string
	}{
		{
			name:     "single quoted pair",
			raw:      "'+7', '7'",
			wantArg:  "+7",
			wantArg2: "7",
		},
		{
			name:     "comma inside single quotes is not a separator",
			raw:      "',', ';'",
			wantArg:  ",",
			wantArg2: ";",
		},
		{
			name:     "single arg in quotes",
			raw:      "'2006-01-02'",
			wantArg:  "2006-01-02",
			wantArg2: "",
		},
		{
			name:     "single arg unquoted",
			raw:      "2006-01-02",
			wantArg:  "2006-01-02",
			wantArg2: "",
		},
		{
			name:     "double quotes are NOT stripped",
			raw:      `"+7", "7"`,
			wantArg:  `"+7"`,
			wantArg2: `"7"`,
		},
		{
			name:     "empty new arg",
			raw:      "'+7', ''",
			wantArg:  "+7",
			wantArg2: "",
		},
		{
			name:     "whitespace around args",
			raw:      "  'a' ,  'b'  ",
			wantArg:  "a",
			wantArg2: "b",
		},
		{
			name:     `unicode escape \uXXXX interpreted`,
			raw:      `'\u0011', '_'`,
			wantArg:  "\u0011",
			wantArg2: "_",
		},
		{
			name:     "escaped backslash",
			raw:      `'\\', '/'`,
			wantArg:  `\`,
			wantArg2: "/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotArg, gotArg2 := parseMethodArgs(tc.raw)
			if gotArg != tc.wantArg || gotArg2 != tc.wantArg2 {
				t.Fatalf("parseMethodArgs(%q) = (%q, %q); want (%q, %q)",
					tc.raw, gotArg, gotArg2, tc.wantArg, tc.wantArg2)
			}
		})
	}
}

func TestIfElse(t *testing.T) {
	tests := []struct {
		name     string
		template string
		ctx      Context
		want     string
	}{
		{
			name:     "if true renders body",
			template: `{{#if data[0] == "yes"}}matched{{/if}}`,
			ctx:      Context{RawFields: []string{"yes"}},
			want:     "matched",
		},
		{
			name:     "if false renders empty",
			template: `{{#if data[0] == "yes"}}matched{{/if}}`,
			ctx:      Context{RawFields: []string{"no"}},
			want:     "",
		},
		{
			name:     "if/else true branch",
			template: `{{#if data[0] == "a"}}A{{#else}}B{{/if}}`,
			ctx:      Context{RawFields: []string{"a"}},
			want:     "A",
		},
		{
			name:     "if/else else branch",
			template: `{{#if data[0] == "a"}}A{{#else}}B{{/if}}`,
			ctx:      Context{RawFields: []string{"x"}},
			want:     "B",
		},
		{
			name:     "if/else if/else first branch",
			template: `{{#if data[0] == "a"}}A{{#else if data[0] == "b"}}B{{#else}}C{{/if}}`,
			ctx:      Context{RawFields: []string{"a"}},
			want:     "A",
		},
		{
			name:     "if/else if/else else-if branch",
			template: `{{#if data[0] == "a"}}A{{#else if data[0] == "b"}}B{{#else}}C{{/if}}`,
			ctx:      Context{RawFields: []string{"b"}},
			want:     "B",
		},
		{
			name:     "if/else if/else else branch",
			template: `{{#if data[0] == "a"}}A{{#else if data[0] == "b"}}B{{#else}}C{{/if}}`,
			ctx:      Context{RawFields: []string{"z"}},
			want:     "C",
		},
		{
			name:     "in operator match",
			template: `{{#if data[0] in ["a", "b"]}}match{{#else}}no{{/if}}`,
			ctx:      Context{RawFields: []string{"b"}},
			want:     "match",
		},
		{
			name:     "in operator no match",
			template: `{{#if data[0] in ["a", "b"]}}match{{#else}}no{{/if}}`,
			ctx:      Context{RawFields: []string{"c"}},
			want:     "no",
		},
		{
			name:     "in operator unquoted values",
			template: `{{#if data[0] in [-10005, -10006]}}yes{{#else}}no{{/if}}`,
			ctx:      Context{RawFields: []string{"-10005"}},
			want:     "yes",
		},
		{
			name:     "!= operator true",
			template: `{{#if data[0] != "x"}}yes{{/if}}`,
			ctx:      Context{RawFields: []string{"y"}},
			want:     "yes",
		},
		{
			name:     "!= operator false",
			template: `{{#if data[0] != "x"}}yes{{/if}}`,
			ctx:      Context{RawFields: []string{"x"}},
			want:     "",
		},
		{
			name:     "numeric > true",
			template: `{{#if data[0] > 5}}big{{#else}}small{{/if}}`,
			ctx:      Context{RawFields: []string{"10"}},
			want:     "big",
		},
		{
			name:     "numeric <= false",
			template: `{{#if data[0] <= 5}}small{{#else}}big{{/if}}`,
			ctx:      Context{RawFields: []string{"10"}},
			want:     "big",
		},
		{
			name:     "numeric >= equal",
			template: `{{#if data[0] >= 5}}yes{{#else}}no{{/if}}`,
			ctx:      Context{RawFields: []string{"5"}},
			want:     "yes",
		},
		{
			name:     "placeholder inside if branch",
			template: `{{#if data[0] == "yes"}}{"val":"{{data[1]}}"}{{#else}}null{{/if}}`,
			ctx:      Context{RawFields: []string{"yes", "hello"}},
			want:     `{"val":"hello"}`,
		},
		{
			name:     "if embedded in larger JSON",
			template: `{"type":{{#if data[0] == "1"}}"primary"{{#else}}"secondary"{{/if}}}`,
			ctx:      Context{RawFields: []string{"1"}},
			want:     `{"type":"primary"}`,
		},
		{
			name:     "nested if blocks - inner match",
			template: `{{#if data[0] == "a"}}{{#if data[1] == "1"}}a1{{#else}}a-other{{/if}}{{#else}}other{{/if}}`,
			ctx:      Context{RawFields: []string{"a", "1"}},
			want:     "a1",
		},
		{
			name:     "nested if blocks - inner else",
			template: `{{#if data[0] == "a"}}{{#if data[1] == "1"}}a1{{#else}}a-other{{/if}}{{#else}}other{{/if}}`,
			ctx:      Context{RawFields: []string{"a", "2"}},
			want:     "a-other",
		},
		{
			name:     "nested if blocks - outer else",
			template: `{{#if data[0] == "a"}}{{#if data[1] == "1"}}a1{{#else}}a-other{{/if}}{{#else}}other{{/if}}`,
			ctx:      Context{RawFields: []string{"b", "1"}},
			want:     "other",
		},
		{
			name:     "XML if/else by msgTypeId - no match falls to else",
			template: `{"ctx":{{#if data.msgTypeId == "-10005"}}{"type":"A"}{{#else if data.msgTypeId == "-10006"}}{"type":"B"}{{#else}}null{{/if}}}`,
			ctx:      Context{RawXML: testXML},
			want:     `{"ctx":null}`,
		},
		{
			name:     "XML if matches predicate value",
			template: `{{#if data.eventContext.param[key=STAT_ID].value == "2"}}active{{#else}}inactive{{/if}}`,
			ctx:      Context{RawXML: testXML},
			want:     "active",
		},
		{
			name:     "numeric quote stripping inside if branch",
			template: `{{#if data[0] == "yes"}}{"n":"{{data[1].toInt()}}"}{{#else}}{}{{/if}}`,
			ctx:      Context{RawFields: []string{"yes", "42"}},
			want:     `{"n":42}`,
		},
		{
			name:     "TSV single-quoted condition value",
			template: `{{#if data[0] == '-10002'}}yes{{#else}}no{{/if}}`,
			ctx:      Context{RawFields: []string{"-10002"}},
			want:     "yes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(tc.template)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := tmpl.Execute(tc.ctx)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestIfElseCompileErrors(t *testing.T) {
	tests := []struct {
		name     string
		template string
	}{
		{
			name:     "unclosed if block",
			template: `{{#if data[0] == "a"}}body`,
		},
		{
			name:     "else without if",
			template: `{{#else}}body{{/if}}`,
		},
		{
			name:     "endif without if",
			template: `{{/if}}`,
		},
		{
			name:     "invalid condition no operator",
			template: `{{#if data[0]}}body{{/if}}`,
		},
		{
			name:     "in without brackets",
			template: `{{#if data[0] in a,b}}body{{/if}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.template)
			if err == nil {
				t.Fatal("expected compile error, got nil")
			}
		})
	}
}

func TestReplaceMethod(t *testing.T) {
	tests := []struct {
		name     string
		template string
		field    string
		want     string
	}{
		{
			name:     "strip plus prefix",
			template: `{"phone":"{{data[0].replace('+7', '7')}}"}`,
			field:    "+77001234567",
			want:     `{"phone":"77001234567"}`,
		},
		{
			name:     "replace comma with semicolon",
			template: `{"list":"{{data[0].replace(',', ';')}}"}`,
			field:    "a,b,c",
			want:     `{"list":"a;b;c"}`,
		},
		{
			name:     "replace space with underscore",
			template: `{"slug":"{{data[0].replace(' ', '_')}}"}`,
			field:    "hello world",
			want:     `{"slug":"hello_world"}`,
		},
		{
			name:     "double quotes are part of search string, no longer a delimiter",
			template: `{{data[0].replace("a", "x")}}`,
			field:    "xab",
			want:     "xab",
		},
		{
			name:     `replace \u0011 control char with underscore`,
			template: `{"v":"{{data[0].replace('\u0011', '_')}}"}`,
			field:    "foo\x11bar",
			want:     `{"v":"foo_bar"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(tc.template)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := tmpl.Execute(Context{RawFields: []string{tc.field}})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// TestToUnix_AlternatingFormats exercises one compiled .toUnix() call across values of
// different date shapes, back to back. .toUnix() caches the dateFormats index that matched
// last time (see timeParser) to skip the scan on a homogeneous stream — this makes sure that
// cache is only ever a hint: a value that doesn't match the cached layout must still fall back
// to the full scan and produce the right answer, every time, without "sticking" to whatever
// layout matched a previous, differently-shaped value.
func TestToUnix_AlternatingFormats(t *testing.T) {
	tmpl, err := Compile(`{{data[0].toUnix()}}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	spaceVal := "2026-08-14 13:27:47"
	wantSpace, err := time.ParseInLocation("2006-01-02 15:04:05", spaceVal, time.Local)
	if err != nil {
		t.Fatalf("reference parse of %q: %v", spaceVal, err)
	}

	rfc3339Val := "2026-08-14T13:27:47+05:00"
	wantRFC3339, err := time.Parse(time.RFC3339, rfc3339Val)
	if err != nil {
		t.Fatalf("reference parse of %q: %v", rfc3339Val, err)
	}

	unixVal := "1723634867"

	seq := []struct {
		val  string
		want string
	}{
		{spaceVal, strconv.FormatInt(wantSpace.Unix(), 10)},
		{rfc3339Val, strconv.FormatInt(wantRFC3339.Unix(), 10)},
		{unixVal, unixVal},
		{spaceVal, strconv.FormatInt(wantSpace.Unix(), 10)},
		{rfc3339Val, strconv.FormatInt(wantRFC3339.Unix(), 10)},
		{spaceVal, strconv.FormatInt(wantSpace.Unix(), 10)},
	}

	for i, step := range seq {
		got, err := tmpl.Execute(Context{RawFields: []string{step.val}})
		if err != nil {
			t.Fatalf("step %d (%q): Execute: %v", i, step.val, err)
		}
		if string(got) != step.want {
			t.Fatalf("step %d (%q): got %q, want %q", i, step.val, got, step.want)
		}
	}
}

// BenchmarkToUnix_Homogeneous models a homogeneous source: every event's date field is
// "2006-01-02 15:04:05" (dateFormats[2]). It benchmarks parseTimeCached directly — the piece
// .toUnix()/.format() actually share and the one the layout-index cache targets — rather than
// the full CompiledTemplate.Execute round trip, whose JSON assembly and the unavoidable
// strconv.FormatInt of the Unix seconds add allocations of their own that have nothing to do
// with date-layout scanning. After warmup, the cache should make this hit dateFormats[2]
// directly on every call: ≲100 ns/op, 0 allocs/op.
func BenchmarkToUnix_Homogeneous(b *testing.B) {
	val := "2026-08-14 13:27:47"
	tc := newTimeParser()

	// Warm the cache: the first call always does the full scan.
	if _, err := parseTimeCached(val, tc); err != nil {
		b.Fatalf("parseTimeCached: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parseTimeCached(val, tc); err != nil {
			b.Fatalf("parseTimeCached: %v", err)
		}
	}
}

// TestJSONStringEscaping_RoundTrip is the JSON escaping round-trip test: a value carrying bytes that are illegal raw inside a JSON string literal (control
// characters, quotes, backslashes, and — as a negative check — non-ASCII text that must pass
// through byte-for-byte) must render as valid JSON that decodes back to the exact original
// string. The first case reproduces the classic failure mode of naive templating:
// "invalid character '\n' in string literal".
func TestJSONStringEscaping_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		val  string
	}{
		{"newline and quote in free text", "Grüße!\nYour \"bonus\" is waiting: {{SHORT_URL}}\n"},
		{"carriage return", "line1\rline2"},
		{"tab", "a\tb"},
		{"backspace 0x08", "a\bb"},
		{"form feed 0x0C", "a\fb"},
		{"control 0x01 (no named escape)", "a\x01b"},
		{"quote and backslash together", `a"b\c`},
		{"non-ASCII and emoji pass through unchanged", "こんにちは 😀 Grüße"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(`{"text1":"{{data.text1}}","text2":""}`)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			rawJSON, err := json.Marshal(map[string]string{"text1": tc.val})
			if err != nil {
				t.Fatalf("json.Marshal input: %v", err)
			}
			got, err := tmpl.Execute(Context{RawJSON: rawJSON})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !json.Valid(got) {
				t.Fatalf("result is not valid JSON: %s", got)
			}
			var v struct {
				Text1 string `json:"text1"`
			}
			if err := json.Unmarshal(got, &v); err != nil {
				t.Fatalf("json.Unmarshal: %v (%s)", err, got)
			}
			if v.Text1 != tc.val {
				t.Fatalf("round-trip mismatch: got %q, want %q", v.Text1, tc.val)
			}
		})
	}
}

// TestJSONStringEscaping_ExactBytes pins the exact escaped output, not just that
// it round-trips — a wrong-but-round-trippable encoding would still be a bug.
func TestJSONStringEscaping_ExactBytes(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want string
	}{
		{"quote and backslash", `a"b\c`, `{"v":"a\"b\\c"}`},
		{"control 0x01 escapes to lowercase \\u0001", "a\x01b", `{"v":"a\u0001b"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(`{"v":"{{data.v}}"}`)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			rawJSON, err := json.Marshal(map[string]string{"v": tc.val})
			if err != nil {
				t.Fatalf("json.Marshal input: %v", err)
			}
			got, err := tmpl.Execute(Context{RawJSON: rawJSON})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestJSONStringEscaping_ObjectPlaceholder_NotEscaped checks that a placeholder that is the
// entire value of a JSON field — not inside quotes — must keep passing its value through as a
// JSON object/array, not get escaped into a string.
func TestJSONStringEscaping_ObjectPlaceholder_NotEscaped(t *testing.T) {
	tmpl, err := Compile(`{"channel_params":{{data.channel_params}}}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := tmpl.Execute(Context{RawJSON: []byte(`{"channel_params":{"a":1,"b":"x\"y"}}`)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !json.Valid(got) {
		t.Fatalf("result is not valid JSON: %s", got)
	}
	var v struct {
		ChannelParams map[string]any `json:"channel_params"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("json.Unmarshal: %v (%s)", err, got)
	}
	if v.ChannelParams == nil {
		t.Fatalf("channel_params did not decode as an object: %s", got)
	}
}

// TestJSONStringEscaping_UnquotedMethodsStayUnquoted is a regression guard on the
// ordering in Compile: stripUnquotedPlaceholderQuotes must run BEFORE markJSONStringContext,
// so toUnix()/toBool() placeholders are seen as already outside a string and render as a bare
// JSON number/boolean, not as an escaped string.
func TestJSONStringEscaping_UnquotedMethodsStayUnquoted(t *testing.T) {
	tmpl, err := Compile(`{"ts":"{{data[0].toUnix()}}","ok":"{{data[1].toBool()}}"}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := tmpl.Execute(Context{RawFields: []string{"2026-08-14 13:27:47", "1"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var v struct {
		Ts int64 `json:"ts"`
		Ok bool  `json:"ok"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("json.Unmarshal: %v (%s) — quotes must have been stripped, not escaped", err, got)
	}
	if !v.Ok {
		t.Fatalf("ok = %v, want true", v.Ok)
	}
}

// TestJSONStringEscaping_EscapedQuoteInLiteral_DoesNotFlipParity checks that an escaped quote
// baked into the template's own literal text (`\"`) must not be mistaken for a string
// boundary by the parity scan — it stays inside the same string as the placeholder that
// follows it.
func TestJSONStringEscaping_EscapedQuoteInLiteral_DoesNotFlipParity(t *testing.T) {
	tmpl, err := Compile(`{"v":"prefix \" {{data.x}}"}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := tmpl.Execute(Context{RawJSON: []byte(`{"x":"a\"b"}`)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !json.Valid(got) {
		t.Fatalf("result is not valid JSON: %s (escaped quote in the template literal likely flipped parity)", got)
	}
	var v struct {
		V string `json:"v"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("json.Unmarshal: %v (%s)", err, got)
	}
	want := `prefix " a"b`
	if v.V != want {
		t.Fatalf("got %q, want %q", v.V, want)
	}
}

// TestJSONStringEscaping_InsideIfBlock checks that a placeholder inside a {{#if}} branch that
// sits within a JSON string must be escaped — in every branch, not just the first.
func TestJSONStringEscaping_InsideIfBlock(t *testing.T) {
	tmpl, err := Compile(`{"v":{{#if data.flag == "1"}}"{{data.a}}"{{#else}}"{{data.b}}"{{/if}}}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	t.Run("if branch", func(t *testing.T) {
		got, err := tmpl.Execute(Context{RawJSON: []byte(`{"flag":"1","a":"x\"y","b":"z"}`)})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !json.Valid(got) {
			t.Fatalf("result is not valid JSON: %s", got)
		}
		var v struct {
			V string `json:"v"`
		}
		if err := json.Unmarshal(got, &v); err != nil {
			t.Fatalf("json.Unmarshal: %v (%s)", err, got)
		}
		if v.V != `x"y` {
			t.Fatalf("got %q, want %q", v.V, `x"y`)
		}
	})

	t.Run("else branch", func(t *testing.T) {
		got, err := tmpl.Execute(Context{RawJSON: []byte(`{"flag":"0","a":"x","b":"p\"q"}`)})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !json.Valid(got) {
			t.Fatalf("result is not valid JSON: %s", got)
		}
		var v struct {
			V string `json:"v"`
		}
		if err := json.Unmarshal(got, &v); err != nil {
			t.Fatalf("json.Unmarshal: %v (%s)", err, got)
		}
		if v.V != `p"q` {
			t.Fatalf("got %q, want %q", v.V, `p"q`)
		}
	})
}

// TestJSONStringEscaping_CompileString_NotEscaped checks that CompileString (key templates,
// source positions) must NOT escape — it isn't JSON, and escaping would corrupt the key.
func TestJSONStringEscaping_CompileString_NotEscaped(t *testing.T) {
	tmpl, err := CompileString(`{{data[0]}}`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	val := "a\"b\nc"
	got, err := tmpl.Execute(Context{RawFields: []string{val}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != val {
		t.Fatalf("got %q, want %q unescaped — CompileString is not JSON", got, val)
	}
}

// BenchmarkNeedsJSONEscape_Clean checks the hot path (a value that needs no escaping) must
// stay 0 allocs/op — needsJSONEscape only scans, writeJSONEscaped's fast path is a plain
// WriteString.
func BenchmarkNeedsJSONEscape_Clean(b *testing.B) {
	s := "15550001234-clean-value-no-special-chars"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if needsJSONEscape(s) {
			b.Fatal("unexpected: clean value reported as needing escape")
		}
	}
}
