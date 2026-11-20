// Command xml maps an XML notification (with namespaces, attributes and repeated elements)
// into JSON.
package main

import (
	"fmt"
	"log"

	"github.com/boostgo/mapperkit"
)

const event = `<?xml version="1.0"?>
<ns:notification xmlns:ns="urn:example:notify" version="2">
  <ns:subscriber>15550001234</ns:subscriber>
  <ns:created>2026-11-20T10:15:00+00:00</ns:created>
  <ns:params>
    <ns:param><ns:key>PLAN</ns:key><ns:value>premium</ns:value></ns:param>
    <ns:param><ns:key>SEGMENT</ns:key><ns:value>7</ns:value></ns:param>
  </ns:params>
</ns:notification>`

const template = `{
  "subscriber": "{{data.subscriber}}",
  "version": "{{data.@version.toInt()}}",
  "created_at": "{{data.created.toUnix()}}",
  "first_param": "{{data.params.param[0].key}}",
  "plan": "{{data.params.param[key=PLAN].value.toUpper()}}",
  "segment": "{{data.params.param[key=SEGMENT].value.toInt()}}"
}`

func main() {
	tmpl, err := mapperkit.Compile(template)
	if err != nil {
		log.Fatal(err)
	}

	// Parse once, reuse for every template evaluated against this event.
	ctx := mapperkit.Context{RawXML: []byte(event)}.WithParsedXML()

	out, err := tmpl.Execute(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(out))
}
