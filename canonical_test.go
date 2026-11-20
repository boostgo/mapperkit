package mapperkit

import (
	"errors"
	"testing"
)

func TestCanonicalize_Rules(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		changed bool
	}{
		{"empty", "", "", false},
		{"none", "none", "none", false},
		{"already canonical single ref", "{{data[4]}}", "{{data[4]}}", false},
		{"already canonical template", "{{data[5]}}-{{data[6]}}", "{{data[5]}}-{{data[6]}}", false},
		{"bare digits", "4", "{{data[4]}}", true},
		{"bracketed index", "[4]", "{{data[4]}}", true},
		{"data-bracket index", "data[4]", "{{data[4]}}", true},
		{"json path", "content.phone", "{{data.content.phone}}", true},
		{"data-prefixed path is NOT special-cased", "data.phone", "{{data.data.phone}}", true},
		{"single word path", "phone", "{{data.phone}}", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := Canonicalize(tc.raw)
			if got != tc.want || changed != tc.changed {
				t.Fatalf("Canonicalize(%q) = (%q, %v); want (%q, %v)", tc.raw, got, changed, tc.want, tc.changed)
			}
		})
	}
}

func TestCanonicalize_Idempotent(t *testing.T) {
	inputs := []string{"", "none", "4", "[4]", "data[4]", "content.phone", "data.phone", "{{data[4]}}", "$$$"}
	for _, raw := range inputs {
		t.Run(raw, func(t *testing.T) {
			once, _ := Canonicalize(raw)
			twice, changedTwice := Canonicalize(once)
			if twice != once || changedTwice {
				t.Fatalf("Canonicalize not idempotent: Canonicalize(%q) = %q, Canonicalize(that) = (%q, %v)", raw, once, twice, changedTwice)
			}
		})
	}
}

func TestValidateRef_EmptyAndNone(t *testing.T) {
	for _, s := range []string{"", "none"} {
		if err := ValidateRef(ModeSingleRef, FormatTSV, s); err != nil {
			t.Fatalf("ValidateRef(%q) = %v; want nil", s, err)
		}
	}
}

func TestValidateRef_ModeSingleRef(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		wantErr error
	}{
		{"single placeholder ok", "{{data[4]}}", nil},
		{"single placeholder with method ok", "{{data.content.phone.trim()}}", nil},
		{"bare literal ok (legal for condition.right)", "1", nil},
		{"two placeholders is not a single ref", "{{data[4]}}-{{data[5]}}", ErrNotSingleRef},
		{"placeholder plus literal is not a single ref", "{{data[4]}} suffix", ErrNotSingleRef},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRef(ModeSingleRef, FormatUnknown, tc.s)
			checkErr(t, err, tc.wantErr)
		})
	}
}

func TestValidateRef_ModeStringTemplate(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		wantErr error
	}{
		{"literal plus placeholders ok", "{{data[5]}}-{{data[6]}}", nil},
		{"literal only ok", "key:static", nil},
		{"if block not allowed", `{{#if data[0] == "1"}}a{{/if}}`, ErrIfBlockNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRef(ModeStringTemplate, FormatUnknown, tc.s)
			checkErr(t, err, tc.wantErr)
		})
	}
}

func TestValidateRef_ModeFullTemplate(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		wantErr error
	}{
		{"if block ok", `{"v":{{#if data[0] == "1"}}1{{#else}}0{{/if}}}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRef(ModeFullTemplate, FormatUnknown, tc.s)
			checkErr(t, err, tc.wantErr)
		})
	}
}

func TestValidateRef_FormatMatrix(t *testing.T) {
	tests := []struct {
		name    string
		format  SourceFormat
		s       string
		wantErr error
	}{
		{"tsv + index ok", FormatTSV, "{{data[4]}}", nil},
		{"tsv + path fails", FormatTSV, "{{data.phone}}", ErrPathBaseOnIndexFormat},
		{"json + path ok", FormatJSON, "{{data.content.phone}}", nil},
		{"json + index fails", FormatJSON, "{{data[4]}}", ErrIndexBaseOnPathFormat},
		{"xml + path ok", FormatXML, "{{data.eventContext.param[key=SUMM].value}}", nil},
		{"xml + index fails", FormatXML, "{{data[4]}}", ErrIndexBaseOnPathFormat},
		{"text + no data refs ok", FormatText, "{{flow_id}}", nil},
		{"text + index fails", FormatText, "{{data[4]}}", ErrDataBaseOnTextFormat},
		{"text + path fails", FormatText, "{{data.phone}}", ErrDataBaseOnTextFormat},
		{"unknown format skips base check", FormatUnknown, "{{data[4]}}", nil},
		{"header not format-checked", FormatJSON, `{{header["X-Trace-Id"]}}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRef(ModeSingleRef, tc.format, tc.s)
			checkErr(t, err, tc.wantErr)
		})
	}
}

func TestValidateRef_NegativeIndex(t *testing.T) {
	err := ValidateRef(ModeSingleRef, FormatUnknown, "{{data[-1]}}")
	checkErr(t, err, ErrInvalidDataIndex)
}

func TestValidateRef_CompileErrorsPropagate(t *testing.T) {
	err := ValidateRef(ModeSingleRef, FormatUnknown, "{{data[abc]}}")
	checkErr(t, err, ErrInvalidDataIndex)
}

func checkErr(t *testing.T, got, want error) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("got error %v; want nil", got)
		}
		return
	}
	if !errors.Is(got, want) {
		t.Fatalf("got error %v; want it to wrap %v", got, want)
	}
}

func TestWithParsedXML_MatchesUnparsedExecute(t *testing.T) {
	rawXML := []byte(`<notifyRequest><phone>15550001234</phone></notifyRequest>`)

	tmpl, err := CompileString("{{data.phone}}")
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}

	want, err := tmpl.Execute(Context{RawXML: rawXML})
	if err != nil {
		t.Fatalf("Execute (unparsed): %v", err)
	}

	preParsed := Context{RawXML: rawXML}.WithParsedXML()
	got, err := tmpl.Execute(preParsed)
	if err != nil {
		t.Fatalf("Execute (pre-parsed): %v", err)
	}

	if got != want {
		t.Fatalf("WithParsedXML result diverges: got %q, want %q", got, want)
	}
}

func TestWithParsedXML_ParsesOnce(t *testing.T) {
	rawXML := []byte(`<notifyRequest><phone>15550001234</phone><clientId>10042</clientId></notifyRequest>`)
	preParsed := Context{RawXML: rawXML}.WithParsedXML()
	if preParsed.parsedXML == nil {
		t.Fatal("WithParsedXML: parsedXML is nil, expected pre-parsed tree")
	}

	phoneTmpl, err := CompileString("{{data.phone}}")
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	clientTmpl, err := CompileString("{{data.clientId}}")
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}

	// Sequential Execute calls on the same pre-parsed Context must reuse the tree —
	// withParsedXML is a no-op once parsedXML is already set, and RawXML is left
	// untouched (only parsedXML is consulted for kindDataField on XML).
	for i := range 3 {
		if got, err := phoneTmpl.Execute(preParsed); err != nil || got != "15550001234" {
			t.Fatalf("iteration %d: phone = %q, err %v", i, got, err)
		}
		if got, err := clientTmpl.Execute(preParsed); err != nil || got != "10042" {
			t.Fatalf("iteration %d: clientId = %q, err %v", i, got, err)
		}
	}
}
