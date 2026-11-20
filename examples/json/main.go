// Command json maps an incoming JSON event into a normalized JSON document.
package main

import (
	"fmt"
	"log"

	"github.com/boostgo/mapperkit"
)

const template = `{
  "event_id": "{{flow_id}}:{{data.order.id}}",
  "customer": "{{data.order.customer.email.toLower()}}",
  "total": "{{data.order.total.toInt()}}",
  "paid": "{{data.order.paid.toBool()}}",
  "created_at": "{{data.order.created.toUnix()}}",
  "first_item": "{{data.order.items[0].sku}}",
  "note": "{{data.order.note}}",
  "tier": {{#if data.order.total >= 1000}}"gold"{{#else if data.order.total >= 100}}"silver"{{#else}}"basic"{{/if}}
}`

func main() {
	tmpl, err := mapperkit.Compile(template)
	if err != nil {
		log.Fatal(err)
	}

	event := []byte(`{"order":{
		"id": "A-17",
		"customer": {"email": "Alice@Example.COM"},
		"total": "149.90",
		"paid": "1",
		"created": "2026-11-20T10:00:00Z",
		"items": [{"sku": "SKU-1"}, {"sku": "SKU-2"}],
		"note": "leave at the \"blue\" door\nthanks"
	}}`)

	out, err := tmpl.Execute(mapperkit.Context{FlowId: "orders", RawJSON: event})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(out))
}
