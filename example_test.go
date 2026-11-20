package mapperkit_test

import (
	"errors"
	"fmt"

	"github.com/boostgo/mapperkit"
)

func ExampleCompile_json() {
	tmpl, err := mapperkit.Compile(`{"user":"{{data.user.name.toUpper()}}","age":"{{data.user.age.toInt()}}"}`)
	if err != nil {
		panic(err)
	}

	out, err := tmpl.Execute(mapperkit.Context{
		RawJSON: []byte(`{"user":{"name":"alice","age":"42"}}`),
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(string(out))
	// Output: {"user":"ALICE","age":42}
}

func ExampleCompile_tsv() {
	tmpl, _ := mapperkit.Compile(`{"id":"{{data[0]}}","host":"{{data[2].host()}}","ok":"{{data[1].toBool()}}"}`)

	out, _ := tmpl.Execute(mapperkit.Context{
		RawFields: []string{"evt-1", "1", "https://example.com:8443/path"},
	})

	fmt.Println(string(out))
	// Output: {"id":"evt-1","host":"example.com","ok":true}
}

func ExampleCompile_xml() {
	tmpl, _ := mapperkit.Compile(`{"phone":"{{data.phone}}","service":"{{data.params.param[key=SERVICE].value}}"}`)

	xml := []byte(`<ns:event xmlns:ns="urn:x">
  <ns:phone>15550001234</ns:phone>
  <ns:params>
    <ns:param><ns:key>SERVICE</ns:key><ns:value>42</ns:value></ns:param>
  </ns:params>
</ns:event>`)

	out, _ := tmpl.Execute(mapperkit.Context{RawXML: xml})

	fmt.Println(string(out))
	// Output: {"phone":"15550001234","service":"42"}
}

func ExampleCompile_conditional() {
	tmpl, _ := mapperkit.Compile(
		`{"level":{{#if data.code >= 500}}"error"{{#else if data.code >= 400}}"warn"{{#else}}"info"{{/if}}}`)

	for _, code := range []string{"503", "404", "200"} {
		out, _ := tmpl.Execute(mapperkit.Context{RawJSON: []byte(`{"code":` + code + `}`)})
		fmt.Println(string(out))
	}
	// Output:
	// {"level":"error"}
	// {"level":"warn"}
	// {"level":"info"}
}

func ExampleCompile_escaping() {
	tmpl, _ := mapperkit.Compile(`{"text":"{{data[0]}}"}`)

	out, _ := tmpl.Execute(mapperkit.Context{RawFields: []string{"line 1\nsaid \"hi\""}})

	fmt.Println(string(out))
	// Output: {"text":"line 1\nsaid \"hi\""}
}

func ExampleCompileString() {
	key, _ := mapperkit.CompileString(`{{flow_id}}:{{header["X-Tenant"]}}:{{data[0].substr(0, 3)}}`)

	s, _ := key.Execute(mapperkit.Context{
		FlowId:    "orders",
		Headers:   map[string]string{"X-Tenant": "acme"},
		RawFields: []string{"ABCDEF"},
	})

	fmt.Println(s)
	// Output: orders:acme:ABC
}

func ExampleCanonicalize() {
	for _, raw := range []string{"4", "[4]", "user.name", "{{data[1]}}"} {
		c, changed := mapperkit.Canonicalize(raw)
		fmt.Println(c, changed)
	}
	// Output:
	// {{data[4]}} true
	// {{data[4]}} true
	// {{data.user.name}} true
	// {{data[1]}} false
}

func ExampleValidateRef() {
	err := mapperkit.ValidateRef(mapperkit.ModeSingleRef, mapperkit.FormatTSV, "{{data.user}}")
	fmt.Println(errors.Is(err, mapperkit.ErrPathBaseOnIndexFormat))

	err = mapperkit.ValidateRef(mapperkit.ModeStringTemplate, mapperkit.FormatJSON, "id-{{data.id}}")
	fmt.Println(err)
	// Output:
	// true
	// <nil>
}

func ExampleContext_WithParsedXML() {
	ctx := mapperkit.Context{RawXML: []byte(`<e><a>1</a><b>2</b></e>`)}.WithParsedXML()

	a, _ := mapperkit.CompileString("{{data.a}}")
	b, _ := mapperkit.CompileString("{{data.b}}")

	va, _ := a.Execute(ctx) // the XML is parsed only once
	vb, _ := b.Execute(ctx)
	fmt.Println(va, vb)
	// Output: 1 2
}
