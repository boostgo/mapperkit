package mapperkit

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestMethodChain_TwoAndThreeMethods covers chains of 2 and 3 methods across all three
// source kinds (TSV/data[N], JSON/data.field, XML). Single-method chains already have
// coverage elsewhere (TestReplaceMethod etc.) and must keep passing unchanged — this file
// is additive, no existing test is touched.
func TestMethodChain_TwoAndThreeMethods(t *testing.T) {
	t.Run("TSV: two methods", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data[0].trim().toUpper()}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawFields: []string{"  hello  "}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":"HELLO"}` {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("TSV: three methods", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data[0].trim().toLower().replace(' ', '_')}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawFields: []string{"  HELLO WORLD  "}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":"hello_world"}` {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("JSON: two methods", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data.name.trim().toUpper()}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawJSON: []byte(`{"name":"  bob  "}`)})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":"BOB"}` {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("JSON: three methods", func(t *testing.T) {
		// substr(5).hexToInt() alone is 2 methods; a leading no-op trim() makes it 3.
		tmpl, err := Compile(`{"v":{{data.cellID.trim().substr(5).hexToInt()}}}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawJSON: []byte(`{"cellID":"ABCDE1A"}`)})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":26}` {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("XML: two methods", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data.phone.trim().toUpper()}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawXML: testXML})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":"15550001234"}` {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("XML: three methods via predicate", func(t *testing.T) {
		tmpl, err := Compile(`{"v":{{data.eventContext.param[key=STAT_ID].value.trim().toInt()}}}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawXML: testXML})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":2}` {
			t.Fatalf("got %q", got)
		}
	})
}

// TestMethodChain_QuotedArgsWithDotAndComma proves a quoted argument containing a literal
// dot and comma is never mistaken for a method boundary or an argument separator.
func TestMethodChain_QuotedArgsWithDotAndComma(t *testing.T) {
	tmpl, err := Compile(`{"v":"{{data[0].replace('a.b', 'x,y').toUpper()}}"}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := tmpl.Execute(Context{RawFields: []string{"1a.b2a.b3"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(got) != `{"v":"1X,Y2X,Y3"}` {
		t.Fatalf("got %q", got)
	}
}

// TestSubstrHexToInt_RealCellID covers a realistic case: a cell id has a 5-char prefix to
// drop before the remainder is parsed as hex — sanity-checked against Go's own
// ParseInt(cellID[5:], 16, 64).
func TestSubstrHexToInt_RealCellID(t *testing.T) {
	const cellID = "26801" + "1A2B3C"

	tmpl, err := CompileString("{{data[0].substr(5).hexToInt()}}")
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	got, err := tmpl.Execute(Context{RawFields: []string{cellID}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	n, err := strconv.ParseInt(cellID[5:], 16, 64)
	if err != nil {
		t.Fatalf("reference ParseInt(%q, 16, 64): %v", cellID[5:], err)
	}
	want := strconv.FormatInt(n, 10)
	if got != want {
		t.Fatalf("got %q, want %q (ParseInt(%q, 16, 64))", got, want, cellID[5:])
	}
}

func TestToBool_Table(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"true", "true"},
		{"false", "false"},
		{"TRUE", "true"},
		{"False", "false"},
		{"1", "true"},
		{"0", "false"},
		{"7", "true"},
		{"-3", "false"},
		{"1.0", "true"},
		{"0.5", "false"},
		{"", "false"},
		{"garbage", "false"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			tmpl, err := CompileString("{{data[0].toBool()}}")
			if err != nil {
				t.Fatalf("CompileString: %v", err)
			}
			got, err := tmpl.Execute(Context{RawFields: []string{tc.in}})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%q.toBool() = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSubstr_Table(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"from only", "{{data[0].substr(2)}}", "cdef"},
		{"from and len", "{{data[0].substr(2, 3)}}", "cde"},
		{"negative from", "{{data[0].substr(-2)}}", "ef"},
		{"from out of range", "{{data[0].substr(10)}}", ""},
		{"len clipped to string length", "{{data[0].substr(2, 99)}}", "cdef"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := CompileString(tc.template)
			if err != nil {
				t.Fatalf("CompileString: %v", err)
			}
			got, err := tmpl.Execute(Context{RawFields: []string{"abcdef"}})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubstr_InvalidArg_CompileError(t *testing.T) {
	_, err := CompileString("{{data[0].substr(abc)}}")
	if err == nil {
		t.Fatal("CompileString: want error for non-numeric substr argument")
	}
}

func TestHexToInt_Table(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// 0x01D2A3 = 119459 (verified against strconv.ParseInt("01D2A3", 16, 64) below).
		{"01D2A3", "119459"},
		{"0x1F", "31"},
		{"", "0"},
		{"zzz", "0"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			tmpl, err := CompileString("{{data[0].hexToInt()}}")
			if err != nil {
				t.Fatalf("CompileString: %v", err)
			}
			got, err := tmpl.Execute(Context{RawFields: []string{tc.in}})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%q.hexToInt() = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHost_Table(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://shop.example.com/shop?a=1", "shop.example.com"},
		{"shop.example.com:443", "shop.example.com"},
		{"shop.example.com", "shop.example.com"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			tmpl, err := CompileString("{{data[0].host()}}")
			if err != nil {
				t.Fatalf("CompileString: %v", err)
			}
			got, err := tmpl.Execute(Context{RawFields: []string{tc.in}})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%q.host() = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestUnquotedByLastMethodInChain proves quote-stripping looks at the LAST method in a
// chain, not the first: hexToInt/toBool trailing a chain unquotes the JSON output, but a
// string method trailing an unquoted-type method keeps it quoted.
func TestUnquotedByLastMethodInChain(t *testing.T) {
	t.Run("hexToInt last -> unquoted number", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data[0].substr(2).hexToInt()}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawFields: []string{"001A"}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		var v map[string]any
		if err := json.Unmarshal(got, &v); err != nil {
			t.Fatalf("result is not valid JSON: %v (%s)", err, got)
		}
		if _, isNumber := v["v"].(float64); !isNumber {
			t.Fatalf("v = %T(%v), want a JSON number (unquoted)", v["v"], v["v"])
		}
	})

	t.Run("toBool last -> unquoted boolean", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data[0].trim().toBool()}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawFields: []string{" 1 "}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":true}` {
			t.Fatalf("got %q, want unquoted JSON boolean", got)
		}
	})

	t.Run("string method last -> stays quoted even though hexToInt appears earlier", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data[0].hexToInt().format('2006')}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawFields: []string{"1A"}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.HasPrefix(string(got), `{"v":"`) {
			t.Fatalf("got %q, want the value to stay quoted (last method is .format(), a string method)", got)
		}
	})
}

// TestMethodChain_TooManyMethods_CompileError enforces the 5-method chain cap.
func TestMethodChain_TooManyMethods_CompileError(t *testing.T) {
	_, err := CompileString("{{data[0].trim().trim().trim().trim().trim().trim()}}") // 6 methods
	if err == nil {
		t.Fatal("CompileString: want error for a 6-method chain (max 5)")
	}
}

// TestMethodChain_FiveMethods_Ok proves the boundary itself (exactly 5) still compiles.
func TestMethodChain_FiveMethods_Ok(t *testing.T) {
	tmpl, err := CompileString("{{data[0].trim().toLower().toUpper().trim().toLower()}}")
	if err != nil {
		t.Fatalf("CompileString: want 5 chained methods to compile, got: %v", err)
	}
	got, err := tmpl.Execute(Context{RawFields: []string{"  HeLLo  "}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

// TestMethodCallDetection_NotMistakenForPathsOrPredicates: neither XML predicates nor
// gjson-array paths end in "(", so extractMethods must leave them untouched — regression
// guard in addition to the implicit coverage in TestXMLPredicateMapping.
func TestMethodCallDetection_NotMistakenForPathsOrPredicates(t *testing.T) {
	t.Run("XML predicate", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data.eventContext.param[key=SERV_ID].value}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawXML: testXML})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":"1046"}` {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("gjson array path", func(t *testing.T) {
		tmpl, err := Compile(`{"v":"{{data.a[0].b}}"}`)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		got, err := tmpl.Execute(Context{RawJSON: []byte(`{"a":[{"b":"first"}]}`)})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if string(got) != `{"v":"first"}` {
			t.Fatalf("got %q", got)
		}
	})
}

// TestAcceptance_TSVTemplate is an end-to-end TSV scenario: a cell id (position 7) has a 5-char prefix to
// drop before the hex remainder is parsed as a decimal cell id, and source_params.host
// (position 14) must come out as a bare host, no scheme/path/port.
func TestAcceptance_TSVTemplate(t *testing.T) {
	tmpl, err := Compile(`{"location":{"lac":"","ci":{{data[7].substr(5).hexToInt()}}},"source_params":{"host":"{{data[14].host()}}"}}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	fields := make([]string, 15)
	fields[7] = "26801" + "1A2B3C" // cellID: 5-char prefix + hex remainder
	fields[14] = "https://shop.example.com/shop?a=1"

	got, err := tmpl.Execute(Context{RawFields: fields})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var v struct {
		Location struct {
			Lac string `json:"lac"`
			Ci  int64  `json:"ci"`
		} `json:"location"`
		SourceParams struct {
			Host string `json:"host"`
		} `json:"source_params"`
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("result is not valid JSON: %v (%s)", err, got)
	}

	wantCi, _ := strconv.ParseInt("1A2B3C", 16, 64)
	if v.Location.Ci != wantCi {
		t.Fatalf("location.ci = %d, want %d (decimal, unquoted)", v.Location.Ci, wantCi)
	}
	if v.SourceParams.Host != "shop.example.com" {
		t.Fatalf("source_params.host = %q, want %q", v.SourceParams.Host, "shop.example.com")
	}
}
