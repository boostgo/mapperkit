// Package mapperkit is a small, fast template engine that maps raw events (TSV fields, JSON,
// XML, HTTP headers/cookies/query parameters) into JSON documents or plain strings.
//
// Templates are compiled once and executed many times; execution is safe for concurrent use.
//
// # Placeholders
//
//	{{data[N]}}             N-th positional field (Context.RawFields), "" if out of range
//	{{data.path}}           JSON path (gjson syntax, a[0] is accepted as a.0) or, without JSON,
//	                        an XML path: "a.b", "item[0].x", "param[key=ID].value", "node.@attr";
//	                        namespace prefixes are ignored and the root element may be omitted
//	{{header["name"]}}      Context.Headers
//	{{cookie["name"]}}      Context.Cookies
//	{{query["name"]}}       Context.QueryParams
//	{{flow_id}}             Context.FlowId
//	{{timestamp}}           current Unix time in seconds
//	{{uuid}}                a random UUID v4
//
// # Methods
//
// Methods are chained left to right (at most 5 per placeholder):
//
//	.format('2006-01-02')     parse a date and reformat it with a Go layout
//	.toUnix()                 parse a date and return Unix seconds (rendered as a JSON number)
//	.unixFormat('layout')     Unix seconds -> formatted UTC date (default "2006-01-02 15:04:05")
//	.toInt()                  integer (floats are truncated, garbage is 0; JSON number)
//	.hexToInt()               hex string (optional 0x) -> decimal (JSON number)
//	.toBool()                 "true"/"false"/number >= 1 -> boolean (JSON boolean)
//	.toLower() .toUpper() .trim()
//	.replace('old', 'new')    replace all occurrences
//	.substr(from[, len])      rune-based substring; negative from counts from the end
//	.host()                   strip scheme, path and port from a URL
//
// Dates are recognized in RFC 3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05",
// "2006-01-02", "02.01.2006[ 15:04:05]", "01/02/2006[ 15:04:05]", RFC 1123, RFC 822 and as Unix
// seconds.
//
// # Conditionals
//
//	{{#if data.status == "ok"}}...{{#else if data.code >= 500}}...{{#else}}...{{/if}}
//
// Operators: ==, !=, >, <, >=, <= (numeric when both sides are numbers, lexicographic otherwise)
// and in, e.g. data.kind in ["a", "b"].
//
// # JSON output
//
// Compile treats the template as text, not as JSON, but understands JSON strings:
//
//   - a placeholder inside a JSON string literal is escaped, so values with quotes, backslashes
//     or control characters always produce valid JSON;
//   - a placeholder that is the whole content of a JSON string and ends with toInt, toUnix,
//     hexToInt or toBool loses its quotes and renders as a bare number or boolean;
//   - a placeholder outside quotes is inserted as is (e.g. a nested JSON object).
//
// CompileString never escapes: use it for keys and other non-JSON strings.
//
// # Validation helpers
//
// Canonicalize turns short-hand references ("4", "[4]", "a.b") into placeholders, and
// ValidateRef checks a value against a RefMode and a SourceFormat before it is stored.
package mapperkit
