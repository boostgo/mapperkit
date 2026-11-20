package mapperkit

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

type xmlNode struct {
	LocalName string
	Attrs     map[string]string
	Children  []*xmlNode
	Text      string
}

// parseXMLToTree decodes XML into a simple node tree, stripping namespace prefixes.
// Go's encoding/xml provides Name.Local automatically, so no manual stripping is needed.
func parseXMLToTree(data []byte) (*xmlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))

	var stack []*xmlNode
	var root *xmlNode

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{
				LocalName: t.Name.Local,
				Attrs:     make(map[string]string, len(t.Attr)),
			}
			for _, a := range t.Attr {
				node.Attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			} else {
				root = node
			}
			stack = append(stack, node)

		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}

		case xml.CharData:
			if len(stack) > 0 {
				if text := strings.TrimSpace(string(t)); text != "" {
					stack[len(stack)-1].Text += text
				}
			}
		}
	}

	return root, nil
}

type xmlSeg struct {
	name      string
	index     int // -1 = no index
	isAttr    bool
	predKey   string // child element name for predicate: param[key=SERV_ID]
	predValue string // expected child text for predicate
}

// parseXMLPath splits a dot-separated path into segments.
//
// Supported bracket forms inside tag[...]:
//   - [N]          — integer index (0-based)
//   - [key=VALUE]  — child-element predicate: keep nodes where child <key> text == VALUE
//
// @attr at the start of a segment selects an attribute of the current node.
func parseXMLPath(path string) []xmlSeg {
	parts := strings.Split(path, ".")
	segs := make([]xmlSeg, 0, len(parts))

	for _, p := range parts {
		if p == "" {
			continue
		}

		seg := xmlSeg{index: -1}

		if strings.HasPrefix(p, "@") {
			seg.isAttr = true
			seg.name = p[1:]
			segs = append(segs, seg)
			continue
		}

		if open := strings.IndexByte(p, '['); open != -1 {
			if close := strings.IndexByte(p[open:], ']'); close > 0 {
				seg.name = p[:open]
				inner := p[open+1 : open+close]
				if n, err := strconv.Atoi(inner); err == nil {
					seg.index = n
				} else if eq := strings.IndexByte(inner, '='); eq > 0 {
					seg.predKey = inner[:eq]
					seg.predValue = inner[eq+1:]
				}
			} else {
				seg.name = p
			}
		} else {
			seg.name = p
		}

		segs = append(segs, seg)
	}

	return segs
}

// ParseXMLTree parses XML bytes into an opaque tree handle.
// Pass the result to NavigateXMLTree to extract field values.
func ParseXMLTree(data []byte) (any, error) {
	return parseXMLToTree(data)
}

// ResolveXMLPosition resolves a position expression against a pre-parsed XML tree.
// Position may be a raw path ("phone") or a template expression ("{{data.phone}}").
// rawXML is only used when position contains template placeholders.
func ResolveXMLPosition(tree any, rawXML []byte, position string) string {
	if position == "" {
		return ""
	}
	node, ok := tree.(*xmlNode)
	if !ok {
		return ""
	}
	if !strings.Contains(position, "{{") {
		return navigateXMLTree(node, position)
	}
	compiled, err := CompileString(position)
	if err != nil || compiled == nil {
		return ""
	}
	val, _ := compiled.Execute(Context{RawXML: rawXML, parsedXML: node})
	return val
}

// NavigateXMLTree resolves a dot-path against a tree returned by ParseXMLTree.
// Returns an empty string if the path is not found, tree is nil, or wrong type.
func NavigateXMLTree(tree any, path string) string {
	if tree == nil {
		return ""
	}
	root, ok := tree.(*xmlNode)
	if !ok {
		return ""
	}
	return navigateXMLTree(root, path)
}

// navigateXMLTree resolves a dot-path against the parsed XML tree.
//
// Traversal rules:
//   - If the current node's LocalName matches the segment, it is "passed through" as-is.
//   - Otherwise the matching children are collected.
//   - This means the root element name can be included or omitted in the path.
//
// Examples (all equivalent given root = "notifyRequest"):
//
//	"phone"                              → text of <phone> (root skipped)
//	"notifyRequest.phone"             → same (explicit root)
//	"eventContext.param[0].key"           → text of first <param>'s <key>
//	"eventContext.param[0].@id"           → attribute id of first <param>
//	"eventContext.param[key=SERV_ID].value" → value of param whose <key> == "SERV_ID"
func navigateXMLTree(root *xmlNode, path string) string {
	segs := parseXMLPath(path)
	if len(segs) == 0 || root == nil {
		return ""
	}

	current := []*xmlNode{root}

	for _, seg := range segs {
		if len(current) == 0 {
			return ""
		}

		if seg.isAttr {
			return current[0].Attrs[seg.name]
		}

		var next []*xmlNode
		for _, node := range current {
			if node.LocalName == seg.name {
				// node itself matches — used for root pass-through
				next = append(next, node)
			} else {
				for _, child := range node.Children {
					if child.LocalName == seg.name {
						next = append(next, child)
					}
				}
			}
		}

		switch {
		case seg.index >= 0:
			if seg.index >= len(next) {
				return ""
			}
			current = []*xmlNode{next[seg.index]}
		case seg.predKey != "":
			var filtered []*xmlNode
			for _, node := range next {
				for _, child := range node.Children {
					if child.LocalName == seg.predKey && child.Text == seg.predValue {
						filtered = append(filtered, node)
						break
					}
				}
			}
			current = filtered
		default:
			current = next
		}
	}

	if len(current) == 0 {
		return ""
	}
	return current[0].Text
}
