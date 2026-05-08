package main

import "fmt"

// docs for hover/completion. These mirror the v0.13 esque language reference.

var keywordDocs = map[string]string{
	"fn":     "Function declaration. Form: `fn name[ShapeParams](params) -> RetType = expr` or `{ block }`.",
	"let":    "Local binding. Form: `let name [: type] = expr;`.",
	"if":     "Conditional expression. `if cond { ... } else { ... }`. Both branches must produce the same type.",
	"else":   "`else` branch of an `if` expression.",
	"match":  "Pattern match expression. `match scrutinee { pat [if guard] => expr, ... }`.",
	"true":   "Boolean literal `true`.",
	"false":  "Boolean literal `false`.",
	"as":     "Postfix numeric cast: `expr as Type`. Defined for `i32 <-> f32`.",
	"in":     "Reserved keyword.",
	"mut":    "Reserved for future linear-types/mutability work; not currently meaningful.",
	"return": "Return from a function. Usually unnecessary — a block's trailing expression is its result.",
}

var builtinDocs = map[string]string{
	"tabulate":      "`tabulate(N, |i| f(i)) -> T[N]` — index-driven tensor construction.",
	"scan":          "`scan(init, |a, x| f(a, x), v: T[N]) -> T[N]` — left prefix accumulator.",
	"iterate":       "`iterate(n, init, f) -> T` — apply `f` `n` times starting from `init`.",
	"iterate_until": "`iterate_until(init, step, pred, max) -> T` — bounded fixpoint.",
	"each":          "`each(v: T[N], f)` — call `f(v[i])` for each element. `f` must be a named fn.",
	"print_i32":     "@io fn print_i32(x: i32) -> i32 — print signed integer to stdout, return x.",
	"print_f32":     "@io fn print_f32(x: f32) -> f32 — print float to stdout (6 fractional digits), return x.",
	"print_str":     "@io fn print_str(s: string) -> string — print string verbatim, return s.",
}

var typeDocs = map[string]string{
	"i8":   "8-bit signed integer.",
	"i16":  "16-bit signed integer.",
	"i32":  "32-bit signed integer (default integer type).",
	"i64":  "64-bit signed integer.",
	"u8":   "8-bit unsigned integer.",
	"u16":  "16-bit unsigned integer.",
	"u32":  "32-bit unsigned integer.",
	"u64":  "64-bit unsigned integer.",
	"f32":  "32-bit IEEE-754 float (default float type).",
	"f64":  "64-bit IEEE-754 float.",
	"bool": "Boolean (`true` or `false`).",
	"unit": "Unit type — a single value, used for statement-position results.",
	"nat":  "Kind of shape parameters (non-negative natural numbers known at compile time).",
}

var operatorDocs = map[string]string{
	"|>":  "Pipeline. `x |> f` desugars to `f(x)`. `x |> f(y)` desugars to `f(x, y)`. Left associative.",
	"+/":  "Sum reduction over a tensor.",
	"*/":  "Product reduction over a tensor.",
	"-/":  "Subtraction reduction over a tensor.",
	".+":  "Element-wise tensor addition.",
	".-":  "Element-wise tensor subtraction.",
	".*":  "Element-wise tensor multiplication.",
	"./":  "Element-wise tensor division.",
	".%":  "Element-wise tensor modulo.",
	"@":   "Matrix multiply (parsed; not yet codegened).",
	"..":  "Exclusive integer range: `lo..hi`.",
	"..=": "Inclusive integer range: `lo..=hi`.",
}

var attrDocs = map[string]string{
	"io":     "@io effect: function performs IO. Required to call `print_*` intrinsics.",
	"kernel": "@kernel: GPU entry point (planned PTX/CUDA backend).",
	"grad":   "@grad: schedule autodiff (planned).",
	"inline": "@inline: advise the inliner (planned).",
}

func keywordCompletions() []CompletionItem {
	out := make([]CompletionItem, 0, len(keywordDocs))
	for k, doc := range keywordDocs {
		out = append(out, CompletionItem{
			Label:         k,
			Kind:          CompletionKindKeyword,
			Documentation: doc,
		})
	}
	return out
}

func builtinCompletions() []CompletionItem {
	out := make([]CompletionItem, 0, len(builtinDocs))
	for name, doc := range builtinDocs {
		out = append(out, CompletionItem{
			Label:         name,
			Kind:          CompletionKindFunction,
			Documentation: doc,
		})
	}
	return out
}

func typeCompletions() []CompletionItem {
	out := make([]CompletionItem, 0, len(typeDocs))
	for name, doc := range typeDocs {
		out = append(out, CompletionItem{
			Label:         name,
			Kind:          CompletionKindClass,
			Documentation: doc,
		})
	}
	return out
}

// hoverFor returns markdown hover content for the token, or "" if
// no useful info applies.
func hoverFor(tok *Tok, doc *Document) string {
	switch tok.Kind {
	case TkFn, TkLet, TkIf, TkElse, TkMatch, TkTrue, TkFalse, TkAs, TkIn, TkMut, TkReturn:
		if d, ok := keywordDocs[tok.Lit]; ok {
			return mdKeyword(tok.Lit, d)
		}
	case TkAttr:
		if d, ok := attrDocs[tok.Lit]; ok {
			return mdKeyword("@"+tok.Lit, d)
		}
		return mdKeyword("@"+tok.Lit, "Attribute (user-defined or unknown).")
	case TkPipeArrow, TkPlusSlash, TkStarSlash, TkMinusSlash,
		TkDotPlus, TkDotMinus, TkDotStar, TkDotSlash, TkDotPercent,
		TkAt, TkDotDot, TkDotDotEq:
		if d, ok := operatorDocs[tok.Lit]; ok {
			return mdKeyword(tok.Lit, d)
		}
	case TkIdent:
		// Type, builtin, or local function.
		if d, ok := typeDocs[tok.Lit]; ok {
			return mdKeyword(tok.Lit, d)
		}
		if d, ok := builtinDocs[tok.Lit]; ok {
			return mdKeyword(tok.Lit, d)
		}
		for _, fn := range doc.Index.Functions {
			if fn.Name == tok.Lit {
				sig := fmt.Sprintf("fn %s(%s)", fn.Name, joinParams(fn.Params))
				if len(fn.Attrs) > 0 {
					var prefix string
					for _, a := range fn.Attrs {
						prefix += "@" + a + " "
					}
					sig = prefix + sig
				}
				return mdCode("esque", sig)
			}
		}
	}
	return ""
}

func joinParams(ps []string) string {
	out := ""
	for i, p := range ps {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

func mdKeyword(name, doc string) string {
	return "**`" + name + "`**\n\n" + doc
}

func mdCode(lang, src string) string {
	return "```" + lang + "\n" + src + "\n```"
}
