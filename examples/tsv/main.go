// Command tsv maps tab-separated log lines into JSON and builds a key for each line.
package main

import (
	"bufio"
	"fmt"
	"log"
	"strings"

	"github.com/boostgo/mapperkit"
)

const input = "42\t2026-11-20 10:15:00\thttps://shop.example.com:443/cart?id=1\t0x1F\tTRUE\n" +
	"43\t2026-11-20 10:16:30\thttp://api.example.org/v1/items\t0xFF\t0\n"

func main() {
	body, err := mapperkit.Compile(
		`{"id":"{{data[0].toInt()}}","ts":"{{data[1].toUnix()}}","day":"{{data[1].format('2006-01-02')}}",` +
			`"host":"{{data[2].host()}}","code":"{{data[3].hexToInt()}}","flag":"{{data[4].toBool()}}"}`)
	if err != nil {
		log.Fatal(err)
	}

	key, err := mapperkit.CompileString(`{{data[2].host()}}:{{data[0]}}`)
	if err != nil {
		log.Fatal(err)
	}

	sc := bufio.NewScanner(strings.NewReader(input))
	for sc.Scan() {
		ctx := mapperkit.Context{RawFields: strings.Split(sc.Text(), "\t")}

		k, err := key.Execute(ctx)
		if err != nil {
			log.Fatal(err)
		}

		v, err := body.Execute(ctx)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("%s => %s\n", k, v)
	}
}
