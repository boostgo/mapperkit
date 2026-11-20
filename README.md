# mapperkit

[![Go Reference](https://pkg.go.dev/badge/github.com/boostgo/mapperkit.svg)](https://pkg.go.dev/github.com/boostgo/mapperkit)
[![Go Report Card](https://goreportcard.com/badge/github.com/boostgo/mapperkit)](https://goreportcard.com/report/github.com/boostgo/mapperkit)
[![CI](https://github.com/boostgo/mapperkit/actions/workflows/ci.yml/badge.svg)](https://github.com/boostgo/mapperkit/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**mapperkit** is a small, fast template engine for turning raw events (TSV lines, JSON, XML,
HTTP headers, cookies and query parameters) into clean JSON documents or plain strings,
driven by configuration rather than code.

```text
{"user":"{{data.user.name.toUpper()}}","age":"{{data.user.age.toInt()}}"}
        +  {"user":{"name":"alice","age":"42"}}
        =  {"user":"ALICE","age":42}
```

Write the mapping once as a template, store it next to the rest of your configuration, and let
non-Go people change it without a redeploy.

## Features

- **One syntax for every input**: positional fields (`data[3]`), JSON paths (`data.a.b[0]`,
  powered by [gjson](https://github.com/tidwall/gjson)), XML paths with indexes, predicates and
  attributes (`data.param[key=ID].value`, `data.@version`), `header["…"]`, `cookie["…"]`,
  `query["…"]`, plus `flow_id`, `timestamp` and `uuid`.
- **Chainable methods**: dates (`format`, `toUnix`, `unixFormat`), numbers (`toInt`, `hexToInt`),
  booleans (`toBool`), strings (`trim`, `toLower`, `toUpper`, `replace`, `substr`), URLs (`host`).
- **Conditionals**: `{{#if}} / {{#else if}} / {{#else}} / {{/if}}` with `== != > < >= <= in`.
- **Always-valid JSON**: values inside JSON strings are escaped automatically; integer and boolean
  results render as real JSON numbers/booleans.
- **Compile once, execute many**: compiled templates are immutable and safe for concurrent use;
  the single-placeholder fast path allocates nothing.
- **Validation helpers** for config UIs and APIs: `Canonicalize` and `ValidateRef`.
- Two small dependencies (`gjson`, `google/uuid`), no reflection, no `text/template`.

## Install

```bash
go get github.com/boostgo/mapperkit
```

Requires Go 1.23 or newer.

## Quick start

```go
package main

import (
	"fmt"
	"log"

	"github.com/boostgo/mapperkit"
)

func main() {
	tmpl, err := mapperkit.Compile(`{
	  "id": "{{flow_id}}:{{data.order.id}}",
	  "email": "{{data.order.email.toLower()}}",
	  "total": "{{data.order.total.toInt()}}",
	  "paid": "{{data.order.paid.toBool()}}"
	}`)
	if err != nil {
		log.Fatal(err)
	}

	out, err := tmpl.Execute(mapperkit.Context{
		FlowId:  "orders",
		RawJSON: []byte(`{"order":{"id":"A-17","email":"Alice@Example.COM","total":"149.90","paid":"1"}}`),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(out))
	// {"id":"orders:A-17","email":"alice@example.com","total":149,"paid":true}
}
```

## Examples

### TSV / positional fields

```go
tmpl, _ := mapperkit.Compile(`{"id":"{{data[0]}}","host":"{{data[2].host()}}","ok":"{{data[1].toBool()}}"}`)

out, _ := tmpl.Execute(mapperkit.Context{
	RawFields: []string{"evt-1", "1", "https://example.com:8443/path"},
})
// {"id":"evt-1","host":"example.com","ok":true}
```

An out-of-range index resolves to an empty string, never panics.

### XML

Namespace prefixes are ignored, and the root element may be included or omitted.

```go
tmpl, _ := mapperkit.Compile(`{"phone":"{{data.phone}}","service":"{{data.params.param[key=SERVICE].value}}"}`)

xml := []byte(`<ns:event xmlns:ns="urn:x">
  <ns:phone>15550001234</ns:phone>
  <ns:params><ns:param><ns:key>SERVICE</ns:key><ns:value>42</ns:value></ns:param></ns:params>
</ns:event>`)

out, _ := tmpl.Execute(mapperkit.Context{RawXML: xml})
// {"phone":"15550001234","service":"42"}
```

| Path | Meaning |
|---|---|
| `a.b` | text of `<b>` inside `<a>` |
| `item[1].name` | the second `<item>` (0-based) |
| `param[key=ID].value` | the `<param>` whose child `<key>` has text `ID` |
| `node.@id` | attribute `id` of `<node>` |

When several templates are evaluated against one XML event, parse it once:

```go
ctx := mapperkit.Context{RawXML: raw}.WithParsedXML()
```

### Conditionals

```go
tmpl, _ := mapperkit.Compile(
	`{"level":{{#if data.code >= 500}}"error"{{#else if data.code >= 400}}"warn"{{#else}}"info"{{/if}}}`)
```

Comparisons are numeric when both sides parse as numbers and lexicographic otherwise.
`in` matches against a list: `{{#if data.kind in ["a", "b"]}}…{{/if}}`.

### Strings and keys

`CompileString` produces plain strings (no JSON escaping), which is ideal for message keys,
cache keys and identifiers:

```go
key, _ := mapperkit.CompileString(`{{flow_id}}:{{header["X-Tenant"]}}:{{data[0].substr(0, 3)}}`)
s, _ := key.Execute(mapperkit.Context{
	FlowId:    "orders",
	Headers:   map[string]string{"X-Tenant": "acme"},
	RawFields: []string{"ABCDEF"},
})
// orders:acme:ABC
```

### JSON safety

A placeholder inside a JSON string literal is escaped, so any value produces valid JSON:

```go
tmpl, _ := mapperkit.Compile(`{"text":"{{data[0]}}"}`)
out, _ := tmpl.Execute(mapperkit.Context{RawFields: []string{"line 1\nsaid \"hi\""}})
// {"text":"line 1\nsaid \"hi\""}
```

If a placeholder is the whole content of a JSON string and its last method is `toInt`, `toUnix`,
`hexToInt` or `toBool`, the quotes are dropped and the value becomes a JSON number or boolean.
A placeholder outside quotes is inserted as is, so you can embed a nested JSON object:
`{"payload":{{data.payload}}}`.

### Validating user-supplied references

```go
// "4" -> "{{data[4]}}", "user.name" -> "{{data.user.name}}"
ref, _ := mapperkit.Canonicalize(input)

if err := mapperkit.ValidateRef(mapperkit.ModeSingleRef, mapperkit.FormatTSV, ref); err != nil {
	// errors.Is(err, mapperkit.ErrPathBaseOnIndexFormat), ErrNotSingleRef, ...
}
```

| Mode | Allows |
|---|---|
| `ModeSingleRef` | exactly one placeholder (or a plain literal) |
| `ModeStringTemplate` | literals and placeholders, no `{{#if}}` |
| `ModeFullTemplate` | everything |

Runnable programs live in [`examples/`](examples): `json`, `tsv` and `xml`
(`go run ./examples/json`).

## Reference

### Placeholders

| Placeholder | Source |
|---|---|
| `{{data[N]}}` | `Context.RawFields[N]` |
| `{{data.path}}` | `Context.RawJSON` (gjson path; `a[0]` is accepted) or, if empty, `Context.RawXML` |
| `{{header["name"]}}` | `Context.Headers` |
| `{{cookie["name"]}}` | `Context.Cookies` |
| `{{query["name"]}}` | `Context.QueryParams` |
| `{{flow_id}}` | `Context.FlowId` |
| `{{timestamp}}` | current Unix time, seconds |
| `{{uuid}}` | random UUID v4 |

### Methods

Up to five methods can be chained; each one receives the result of the previous one.
String arguments use single quotes.

| Method | Result | Renders as |
|---|---|---|
| `.format('2006-01-02')` | date reformatted with a Go layout (unchanged if not a date) | string |
| `.toUnix()` | date → Unix seconds (unchanged if not a date) | number |
| `.unixFormat('layout')` | Unix seconds → UTC date; default layout `2006-01-02 15:04:05` | string |
| `.toInt()` | integer; floats truncated; anything else `0` | number |
| `.hexToInt()` | hex (optional `0x`) → decimal; invalid `0` | number |
| `.toBool()` | `true`/`false` (case-insensitive) or number ≥ 1 → `true`; else `false` | boolean |
| `.toLower()`, `.toUpper()`, `.trim()` | case / whitespace | string |
| `.replace('old', 'new')` | replace all occurrences | string |
| `.substr(from)`, `.substr(from, len)` | rune-based substring; negative `from` counts from the end | string |
| `.host()` | strip scheme, path and port from a URL | string |

Recognized date formats: RFC 3339, `2006-01-02T15:04:05`, `2006-01-02 15:04:05`, `2006-01-02`,
`02.01.2006[ 15:04:05]`, `01/02/2006[ 15:04:05]`, RFC 1123, RFC 822 and Unix seconds.
Dates without a zone are parsed in `time.Local`.

### Errors

All errors wrap a sentinel (`ErrCompile`, `ErrUnclosedPlaceholder`, `ErrUnknownPlaceholder`,
`ErrUnknownMethod`, `ErrIfBlockSyntax`, `ErrTooManyMethods`, `ErrInvalidMethodArg`, …), so use
`errors.Is`. See the [package documentation](https://pkg.go.dev/github.com/boostgo/mapperkit)
for the full list.

### Versioning

`mapperkit.Version()` returns the engine version. Services that validate templates in one place
and execute them in another can compare it to make sure both run the same semantics.

## Contributing

Issues and pull requests are welcome. Before sending a PR:

```bash
make test   # go vet + go test -race
make lint   # golangci-lint
make cover  # coverage report
```

Please add a test (and, for user-visible behavior, an `Example`) for every change, and update
[CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE) © 2026 boostgo
