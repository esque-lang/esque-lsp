package main

// FnSymbol is a discovered top-level function definition.
type FnSymbol struct {
	Name    string
	Params  []string
	Range   Range // full fn declaration range (best effort)
	NameRng Range // range of the function name identifier
	IsKernel bool
	Attrs    []string
}

// LetSymbol is a top-level `let` binding inside a block (best effort).
type LetSymbol struct {
	Name    string
	Range   Range
	NameRng Range
}

// FileIndex is a lightweight index of a single document built from the
// token stream. It is intentionally tolerant of syntax errors.
type FileIndex struct {
	Functions []FnSymbol
}

// Index walks the token stream and pulls out function declarations.
//
// The recogniser is purely positional: it spots `fn IDENT` (optionally
// preceded by `@attr` attributes), then captures the parameter list
// and a best-effort body span. It does not attempt to type-check;
// the goal is symbol/definition support, not full semantic analysis.
func Index(toks []Tok) FileIndex {
	var idx FileIndex
	var attrs []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind == TkAttr {
			attrs = append(attrs, t.Lit)
			continue
		}
		if t.Kind == TkFn {
			fn, end := parseFnDecl(toks, i, attrs)
			if fn != nil {
				idx.Functions = append(idx.Functions, *fn)
			}
			attrs = nil
			i = end
			continue
		}
		// Anything else clears the pending attrs.
		if t.Kind == TkEOF {
			break
		}
		if t.Kind != TkAttr && len(attrs) > 0 {
			// Allow attrs to attach across whitespace only — but
			// we have already skipped whitespace in the lexer, so
			// any non-`fn` token after attrs means "discard them".
			attrs = nil
		}
	}
	return idx
}

// parseFnDecl parses a function declaration starting at the `fn`
// token index. Returns the parsed symbol and the index of the last
// token consumed.
func parseFnDecl(toks []Tok, i int, attrs []string) (*FnSymbol, int) {
	// toks[i] is TkFn
	start := toks[i].Range.Start
	if i+1 >= len(toks) || toks[i+1].Kind != TkIdent {
		return nil, i
	}
	nameTok := toks[i+1]
	fn := &FnSymbol{
		Name:    nameTok.Lit,
		NameRng: nameTok.Range,
		Attrs:   append([]string(nil), attrs...),
	}
	for _, a := range attrs {
		if a == "kernel" {
			fn.IsKernel = true
		}
	}
	j := i + 2
	// optional shape parameters [N, M: nat]
	if j < len(toks) && toks[j].Kind == TkLBracket {
		depth := 1
		j++
		for j < len(toks) && depth > 0 {
			switch toks[j].Kind {
			case TkLBracket:
				depth++
			case TkRBracket:
				depth--
			case TkEOF:
				return nil, j
			}
			j++
		}
	}
	// parameters
	if j >= len(toks) || toks[j].Kind != TkLParen {
		fn.Range = Range{Start: start, End: nameTok.Range.End}
		return fn, j - 1
	}
	j++ // past (
	for j < len(toks) && toks[j].Kind != TkRParen {
		if toks[j].Kind == TkIdent {
			fn.Params = append(fn.Params, toks[j].Lit)
		}
		if toks[j].Kind == TkEOF {
			break
		}
		j++
	}
	if j < len(toks) && toks[j].Kind == TkRParen {
		j++
	}
	// `-> RetType`
	if j < len(toks) && toks[j].Kind == TkArrow {
		j++
		j = skipTypeExpr(toks, j)
	}
	// body — either `=` <expr> or `{ ... }`
	end := nameTok.Range.End
	if j < len(toks) {
		switch toks[j].Kind {
		case TkEq:
			j++
			j, end = skipExpr(toks, j)
		case TkLBrace:
			depth := 1
			endTok := toks[j]
			j++
			for j < len(toks) && depth > 0 {
				switch toks[j].Kind {
				case TkLBrace:
					depth++
				case TkRBrace:
					depth--
					endTok = toks[j]
				case TkEOF:
					depth = 0
				}
				j++
			}
			end = endTok.Range.End
		}
	}
	fn.Range = Range{Start: start, End: end}
	return fn, j - 1
}

// skipTypeExpr advances past a type expression (e.g. `f32`,
// `f32[N, M]`). Only knows about brackets; treats anything else as
// the type's end.
func skipTypeExpr(toks []Tok, j int) int {
	if j >= len(toks) {
		return j
	}
	// The type starts with an ident.
	if toks[j].Kind != TkIdent {
		return j
	}
	j++
	if j < len(toks) && toks[j].Kind == TkLBracket {
		depth := 1
		j++
		for j < len(toks) && depth > 0 {
			switch toks[j].Kind {
			case TkLBracket:
				depth++
			case TkRBracket:
				depth--
			case TkEOF:
				return j
			}
			j++
		}
	}
	return j
}

// skipExpr advances over a single expression on a "best effort"
// basis: it stops at the next top-level statement boundary
// (a top-level `fn` or `@attr` keyword, or EOF).
//
// Returns the new index and the end position of the consumed expr.
func skipExpr(toks []Tok, j int) (int, Position) {
	end := Position{}
	depthParen := 0
	depthBrace := 0
	depthBracket := 0
	for j < len(toks) {
		t := toks[j]
		switch t.Kind {
		case TkEOF:
			return j, end
		case TkLParen:
			depthParen++
		case TkRParen:
			if depthParen == 0 {
				return j, end
			}
			depthParen--
		case TkLBrace:
			depthBrace++
		case TkRBrace:
			if depthBrace == 0 {
				return j, end
			}
			depthBrace--
		case TkLBracket:
			depthBracket++
		case TkRBracket:
			if depthBracket == 0 {
				return j, end
			}
			depthBracket--
		case TkFn, TkAttr:
			if depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				return j, end
			}
		}
		end = t.Range.End
		j++
	}
	return j, end
}
