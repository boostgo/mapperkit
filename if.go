package mapperkit

import (
	"fmt"
	"strconv"
	"strings"
)

// ─── Tokenizer ────────────────────────────────────────────────────────────────

type tokenKind int

const (
	tokKindLiteral tokenKind = iota // plain text between {{ }} markers
	tokKindExpr                     // regular {{expr}} placeholder
	tokKindIfOpen                   // {{#if condition}}
	tokKindElseIf                   // {{#else if condition}}
	tokKindElse                     // {{#else}}
	tokKindEndIf                    // {{/if}}
)

type token struct {
	kind tokenKind
	text string // literal text OR condition/placeholder expression
}

func tokenizeTemplate(tmpl string) ([]token, error) {
	var tokens []token
	remaining := tmpl

	for {
		start := strings.Index(remaining, "{{")
		if start == -1 {
			if remaining != "" {
				tokens = append(tokens, token{kind: tokKindLiteral, text: remaining})
			}
			break
		}
		if start > 0 {
			tokens = append(tokens, token{kind: tokKindLiteral, text: remaining[:start]})
		}

		end := strings.Index(remaining[start:], "}}")
		if end == -1 {
			return nil, fmt.Errorf("%w: %q", ErrUnclosedPlaceholder, tmpl)
		}
		end += start

		inner := strings.TrimSpace(remaining[start+2 : end])
		tok, err := classifyToken(inner)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
		remaining = remaining[end+2:]
	}

	return tokens, nil
}

func classifyToken(inner string) (token, error) { //nolint:unparam // error kept for future token kinds
	switch {
	case strings.HasPrefix(inner, "#if "):
		return token{kind: tokKindIfOpen, text: strings.TrimSpace(inner[4:])}, nil
	case inner == "#else":
		return token{kind: tokKindElse}, nil
	case strings.HasPrefix(inner, "#else if "):
		return token{kind: tokKindElseIf, text: strings.TrimSpace(inner[9:])}, nil
	case inner == "/if":
		return token{kind: tokKindEndIf}, nil
	default:
		return token{kind: tokKindExpr, text: inner}, nil
	}
}

// ─── Parser: tokens → parts tree ─────────────────────────────────────────────

// buildPartsFromTokens parses tokens into a parts slice starting at pos.
// Stops at block-control tokens (else-if / else / end-if) and returns the
// unconsumed position so the caller can handle it.
func buildPartsFromTokens(tokens []token, pos int) ([]part, int, error) {
	var parts []part

	for pos < len(tokens) {
		tok := tokens[pos]

		switch tok.kind {
		case tokKindLiteral:
			parts = append(parts, part{literal: tok.text})
			pos++

		case tokKindExpr:
			ph, err := parsePlaceholder(tok.text)
			if err != nil {
				return nil, 0, err
			}
			parts = append(parts, part{placeholder: ph})
			pos++

		case tokKindIfOpen:
			block, newPos, err := buildIfBlock(tokens, pos)
			if err != nil {
				return nil, 0, err
			}
			parts = append(parts, part{ifBlock: block})
			pos = newPos

		case tokKindElseIf, tokKindElse, tokKindEndIf:
			// signal to caller: stop here, let the caller consume this token
			return parts, pos, nil
		}
	}

	return parts, pos, nil
}

// buildIfBlock builds an ifBlock starting at tokens[pos] (tokKindIfOpen).
// Returns the completed block and the position after the matching {{/if}}.
func buildIfBlock(tokens []token, pos int) (*ifBlock, int, error) {
	cond0, err := parseInlineCondition(tokens[pos].text)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: {{#if}}: %w", ErrIfBlockSyntax, err)
	}
	pos++ // consume {{#if ...}}

	block := &ifBlock{}

	bodyParts, newPos, err := buildPartsFromTokens(tokens, pos)
	if err != nil {
		return nil, 0, err
	}
	pos = newPos
	block.branches = append(block.branches, ifBranch{cond: cond0, parts: bodyParts})

	for pos < len(tokens) {
		switch tokens[pos].kind {
		case tokKindElseIf:
			condN, err := parseInlineCondition(tokens[pos].text)
			if err != nil {
				return nil, 0, fmt.Errorf("%w: {{#else if}}: %w", ErrIfBlockSyntax, err)
			}
			pos++ // consume {{#else if ...}}

			branchParts, newPos, err := buildPartsFromTokens(tokens, pos)
			if err != nil {
				return nil, 0, err
			}
			pos = newPos
			block.branches = append(block.branches, ifBranch{cond: condN, parts: branchParts})

		case tokKindElse:
			pos++ // consume {{#else}}

			elseParts, newPos, err := buildPartsFromTokens(tokens, pos)
			if err != nil {
				return nil, 0, err
			}
			pos = newPos
			block.elseParts = elseParts

			if pos >= len(tokens) || tokens[pos].kind != tokKindEndIf {
				return nil, 0, fmt.Errorf("%w: expected {{/if}} after {{#else}}", ErrIfBlockSyntax)
			}
			pos++ // consume {{/if}}
			return block, pos, nil

		case tokKindEndIf:
			pos++ // consume {{/if}}
			return block, pos, nil

		default:
			return nil, 0, fmt.Errorf("%w: unexpected token inside {{#if}} block", ErrIfBlockSyntax)
		}
	}

	return nil, 0, fmt.Errorf("%w: missing {{/if}}", ErrUnclosedIfBlock)
}

// ─── If Block Runtime Types ───────────────────────────────────────────────────

type ifBlock struct {
	branches  []ifBranch
	elseParts []part // nil = no else branch
}

type ifBranch struct {
	cond  *inlineCondition
	parts []part
}

// ─── Inline Condition ─────────────────────────────────────────────────────────

type inlineCondOp int

const (
	inlineOpEq inlineCondOp = iota
	inlineOpNeq
	inlineOpGt
	inlineOpLt
	inlineOpGte
	inlineOpLte
	inlineOpIn
)

type inlineCondition struct {
	left  *CompiledString
	op    inlineCondOp
	right string   // scalar right-hand value (all ops except in)
	list  []string // right-hand list (op == in only)
}

// condOpDefs lists operators longest-first so >= is tried before >.
var condOpDefs = []struct {
	op    inlineCondOp
	token string // surrounded by spaces to avoid partial matches
}{
	{inlineOpGte, " >= "},
	{inlineOpLte, " <= "},
	{inlineOpNeq, " != "},
	{inlineOpEq, " == "},
	{inlineOpGt, " > "},
	{inlineOpLt, " < "},
	{inlineOpIn, " in "},
}

// parseInlineCondition parses a condition expression from {{#if ...}}.
//
// Supported forms:
//
//	data.field == "value"
//	data.field != value
//	data.field >= 10
//	data.field in ["val1", "val2"]
func parseInlineCondition(cond string) (*inlineCondition, error) {
	for _, def := range condOpDefs {
		idx := strings.Index(cond, def.token)
		if idx == -1 {
			continue
		}
		leftStr := strings.TrimSpace(cond[:idx])
		rightStr := strings.TrimSpace(cond[idx+len(def.token):])

		leftCS, err := CompileString("{{" + leftStr + "}}")
		if err != nil {
			return nil, fmt.Errorf("%w: left %q: %w", ErrInvalidIfCondition, leftStr, err)
		}

		ic := &inlineCondition{left: leftCS, op: def.op}

		if def.op == inlineOpIn {
			list, err := parseCondList(rightStr)
			if err != nil {
				return nil, fmt.Errorf("%w: in-list: %w", ErrInvalidIfCondition, err)
			}
			ic.list = list
		} else {
			ic.right = unquoteCondValue(rightStr)
		}

		return ic, nil
	}
	return nil, fmt.Errorf("%w: cannot parse condition %q (missing operator)", ErrInvalidIfCondition, cond)
}

// parseCondList parses "[val1, val2, ...]" and returns unquoted items.
func parseCondList(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("expected [...], got %q", s)
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return nil, nil
	}
	items := strings.Split(inner, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, unquoteCondValue(strings.TrimSpace(item)))
	}
	return result, nil
}

// unquoteCondValue strips one surrounding pair of " or ' delimiters if present.
func unquoteCondValue(s string) string {
	if len(s) >= 2 &&
		((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

// ─── Evaluation ──────────────────────────────────────────────────────────────

// evalInlineCondition evaluates the condition against the template context.
func evalInlineCondition(cond *inlineCondition, ctx Context) bool {
	leftVal, err := cond.left.Execute(ctx)
	if err != nil {
		return false
	}

	if cond.op == inlineOpIn {
		for _, item := range cond.list {
			if leftVal == item {
				return true
			}
		}
		return false
	}

	switch cond.op {
	case inlineOpEq:
		return leftVal == cond.right
	case inlineOpNeq:
		return leftVal != cond.right
	case inlineOpGt, inlineOpLt, inlineOpGte, inlineOpLte:
		l, errL := strconv.ParseFloat(strings.TrimSpace(leftVal), 64)
		r, errR := strconv.ParseFloat(strings.TrimSpace(cond.right), 64)
		if errL == nil && errR == nil {
			switch cond.op {
			case inlineOpGt:
				return l > r
			case inlineOpLt:
				return l < r
			case inlineOpGte:
				return l >= r
			case inlineOpLte:
				return l <= r
			}
		}
		// fall back to lexicographic comparison
		cmp := strings.Compare(leftVal, cond.right)
		switch cond.op {
		case inlineOpGt:
			return cmp > 0
		case inlineOpLt:
			return cmp < 0
		case inlineOpGte:
			return cmp >= 0
		case inlineOpLte:
			return cmp <= 0
		}
	}
	return false
}

// resolveIfBlock evaluates branches in order and renders the first matching one.
// Returns "" when no branch matches and there is no else.
func resolveIfBlock(block *ifBlock, ctx Context) (string, error) {
	for _, branch := range block.branches {
		if evalInlineCondition(branch.cond, ctx) {
			return resolveParts(branch.parts, ctx)
		}
	}
	if block.elseParts != nil {
		return resolveParts(block.elseParts, ctx)
	}
	return "", nil
}
