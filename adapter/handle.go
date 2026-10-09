package adapter

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/page"
)

const handleSig = "func (a *Action) Handle(ctx context.Context, in Input) (Output, error)"

// Statement kinds (S-patterns).
const (
	kPre     = "S1"
	kGuard   = "S2"
	kQuery   = "S3"
	kErrRet  = "S4"
	kLet     = "S5"
	kPost    = "S6"
	kReturn  = "S7"
	kMapEach = "S8"
	kNext    = "S9"
)

func st(key string) string { return stepTemplates[key] }

func statusPhrase(code int) string {
	return fmt.Sprintf(docSentences["status"], code, http.StatusText(code))
}

func (w *walker) internal() string { return statusPhrase(w.env.http.Internal.Status) }

func (w *walker) handle(d *ast.FuncDecl) {
	if got := w.print(&ast.FuncDecl{Recv: d.Recv, Name: d.Name, Type: d.Type}); got != handleSig {
		w.refuse(d, "Handle signature "+got, "D9 handle", "Write exactly: "+handleSig)
		return
	}
	start, end := w.fset.Position(d.Pos()).Line, w.fset.Position(d.End()).Line
	if n := end - start + 1; n > MaxHandleLines {
		w.refuse(d, fmt.Sprintf("Handle of %d lines", n), "hard limit", fmt.Sprintf("At most %d lines", MaxHandleLines))
	}
	for _, in := range w.f.Input {
		if in.List != "" && w.f.Method == "GET" {
			w.errs = append(w.errs, Refusal{Pos: in.pos, Construct: "list field " + in.Name + " in a GET action", Context: "D10 list input",
				Hint: "A GET takes path and query values only; a list input is sent in the JSON body of a POST, PUT, PATCH or DELETE"})
		}
	}
	w.locals = map[string]*local{"a": {kind: "action"}, "ctx": {kind: "context"}, "in": {kind: "request"}}
	stmts := d.Body.List
	phase := 0 // 0 preconditions, 1 body, 2 postconditions, 3 returned
	stopped := false
	// The walk stops at the first statement outside the grammar: everything
	// after it would be rendered out of context, so it is not reported.
walk:
	for i := 0; i < len(stmts); i++ {
		s := stmts[i]
		w.asserts = nil
		if phase == 3 {
			w.refuse(s, "statement after the success return", "Handle body", "S7 is the last statement")
			return
		}
		kind := w.classify(s)
		if phase == 2 && kind != kPost && kind != kReturn && kind != "" {
			w.refuse(s, "statement after a postcondition", "Handle body", "Only S6 postconditions and the S7 return may follow S6")
			continue
		}
		switch kind {
		case kPre:
			if phase != 0 {
				w.refuse(s, "precondition after other statements", "S1 precondition", "Move assert.Pre to the top of Handle")
				continue
			}
			w.precondition(s)
		case kPost:
			phase = 2
			w.post(s)
		case kGuard:
			phase = 1
			w.guard(s.(*ast.IfStmt))
		case kQuery:
			phase = 1
			w.query(s.(*ast.AssignStmt))
			if i+1 < len(stmts) && w.classify(stmts[i+1]) == kErrRet {
				i++
			} else {
				w.refuse(s, "query whose error is not checked", "S3 query", "Follow it with S4: if err != nil { return Output{}, err }")
			}
		case kErrRet:
			w.refuse(s, "error return without a query just before it", "S4 error return", "S4 only directly follows S3")
		case kLet:
			phase = 1
			// S8: items := make([]domain.T, len(rows)); for i, row := range rows { items[i] = ... }
			if isMakeLenAssign(s) && i+1 < len(stmts) {
				if rs, ok := stmts[i+1].(*ast.RangeStmt); ok {
					w.mapEach(s.(*ast.AssignStmt), rs)
					i++
					break
				}
			}
			w.let(s.(*ast.AssignStmt))
		case kNext:
			phase = 1
			w.nextCursor(s.(*ast.AssignStmt))
		case kMapEach:
			w.refuse(s, "map-each for-range without the make([]domain.T, len(rows)) just before it", "S8 map each",
				"Write: items := make([]domain.T, len(rows)); for i, row := range rows { items[i] = domain.T{...} }")
		case kReturn:
			if phase != 2 {
				w.refuse(s, "success return without a postcondition before it", "S7 success return", "Add assert.Post(...) before return")
			}
			w.ret(s.(*ast.ReturnStmt))
			phase = 3
		default:
			hint := "Allowed here: " + allowedStmts
			if _, ok := s.(*ast.ReturnStmt); ok {
				hint = "Success is S7 (return <name>, nil) after S6; failures are S2 guards (return Output{}, F<n>)"
			}
			w.refuse(s, stmtName(s), "Handle body", hint)
			stopped = true
			break walk
		}
	}
	if phase != 3 && !stopped {
		w.refuse(rbrace{d.Body}, "Handle that does not end in a success return", "S7 success return", "End with return <name>, nil")
	}
	if !stopped {
		for _, name := range sortedKeys(w.locals) {
			if loc := w.locals[name]; loc.kind == "changed" && !loc.checked {
				w.uncheckedClaim(name, loc)
			}
		}
	}
}

// uncheckedClaim refuses a claim no guard checks with S10 (or S11). A guard
// that tests the check inside a compound condition (&&, ||, !) does not
// count: claimed == 0 && claimed != 1 is claimed == 0, so a claim that
// changed 2 rows would pass it.
func (w *walker) uncheckedClaim(name string, loc *local) {
	ctx, check := "S10 claim check", name+" != 1"
	if loc.multi {
		ctx, check = "S11 multi-row claim check", name+" != int64(len(in."+loc.list+"))"
	}
	if loc.nested > 0 {
		w.refuse(loc.stmt, fmt.Sprintf("claim whose changed-row count is checked only inside a compound condition (line %d)", loc.nested), ctx,
			"The check is a guard of its own whose entire condition is "+check+": if "+check+" { return Output{}, F<n> }. Inside &&, || or ! it does not stop every wrong count ("+name+" == 0 && "+check+" stops only when no row changed, so a claim that changed too many rows passes); such guards may stay as extra guards")
		return
	}
	if loc.multi {
		w.refuse(loc.stmt, "claim whose changed-row count no guard checks", ctx, strings.NewReplacer("<changed>", name, "<List>", loc.list).Replace(multiCheckHint))
		return
	}
	w.refuse(loc.stmt, "claim whose changed-row count no guard checks", ctx, claimCheckHint)
}

// isClaimCheck reports whether e, through parentheses, is exactly the check
// of claim loc: <n> != 1 (S10), or for a claim over a Q7 IN list
// <n> != int64(len(in.<List>)) with the list its IN is bound to (S11).
func (w *walker) isClaimCheck(e ast.Expr) *local {
	b, ok := unparen(e).(*ast.BinaryExpr)
	if !ok || b.Op != token.NEQ {
		return nil
	}
	id, ok := b.X.(*ast.Ident)
	if !ok {
		return nil
	}
	loc := w.locals[id.Name]
	if loc == nil || loc.kind != "changed" {
		return nil
	}
	if lit, ok := b.Y.(*ast.BasicLit); ok && !loc.multi && lit.Kind == token.INT && lit.Value == "1" {
		return loc
	}
	if call, ok := b.Y.(*ast.CallExpr); ok && loc.multi {
		if name, _, ok := w.lenOfList(call); ok && name == loc.list {
			return loc
		}
	}
	return nil
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// claimCond renders one Q6 WHERE condition (or its OR group).
func (w *walker) claimCond(c sqlCond, vals map[string]string, table string) string {
	if len(c.Any) > 0 {
		parts := make([]string, len(c.Any))
		for i, a := range c.Any {
			parts[i] = w.claimCond(a, vals, table)
		}
		return fmt.Sprintf(t("paren"), strings.Join(parts, " or "))
	}
	if c.Op == "in" {
		return fmt.Sprintf(t("where in"), c.Col, vals[c.Val.Param])
	}
	if v := c.Val; v.Kind == "param" && vals[v.Param] == t("clock") {
		if v.Op == "" {
			return clockNow("`"+c.Col+"`", c.Op)
		}
		n, _ := strconv.Atoi(v.Off)
		return clockComparison(c.Col, c.Op, v.Op, n)
	}
	return fmt.Sprintf(t("where "+c.Op), c.Col, w.sqlValue(c.Val, vals, c.Col, table))
}

// clockComparison renders <col> <op> <now> - n (sign "-") or + n (sign "+")
// as a distance from the current time, so the boundary is exact and the
// direction cannot be misread. With d = n seconds said in words:
//
//	col <= now - d   col is d or more before the current time
//	col <  now - d   col is more than d before the current time
//	col >  now - d   col is later than d before the current time
//	col >= now - d   col is no earlier than d before the current time
//	col >= now + d   col is d or more after the current time
//	col >  now + d   col is more than d after the current time
//	col <  now + d   col is earlier than d after the current time
//	col <= now + d   col is no later than d after the current time
//
// "Later than" / "no earlier than" (and "earlier than" / "no later than")
// order the column against the point d from now, so they plainly include
// every time on the other side of now (RULEBOOK.md, Q6).
func clockComparison(col, op, sign string, seconds int) string {
	return fmt.Sprintf(t("clock "+sign+" "+op), col, clockAmount(seconds), t("clock"))
}

// clockNow renders <value> <op> <now> with no offset in the same boundary
// words as clockComparison, so "no later than" includes the current time
// itself and "earlier than" does not:
//
//	col <= now   col is no later than the current time
//	col <  now   col is earlier than the current time
//	col >  now   col is later than the current time
//	col >= now   col is no earlier than the current time
//	col =  now   col is exactly the current time (<>: is not exactly)
//
// op is SQL (=, <>) or Go (==, !=); value is already rendered.
func clockNow(value, op string) string {
	switch op {
	case "==":
		op = "="
	case "!=":
		op = "<>"
	}
	return fmt.Sprintf(t("clock "+op), value, t("clock"))
}

// offsetPhrase renders a Q6 value "<parameter> + n" or "- n" (in SET). Next
// to the current time (T1) a whole number is seconds, said in the largest
// whole unit.
func offsetPhrase(base, op, off string) string {
	if base != t("clock") {
		return fmt.Sprintf(t("offset "+op), base, off)
	}
	n, _ := strconv.Atoi(off)
	return fmt.Sprintf(t("time "+op), clockAmount(n), base)
}

// clockAmount says n seconds in the largest whole unit: "10 minutes".
func clockAmount(n int) string {
	switch {
	case n == 1:
		return t("1 second")
	case n == 60:
		return t("1 minute")
	case n == 3600:
		return t("1 hour")
	case n == 86400:
		return t("1 day")
	case n > 0 && n%86400 == 0:
		return fmt.Sprintf(t("days"), n/86400)
	case n > 0 && n%3600 == 0:
		return fmt.Sprintf(t("hours"), n/3600)
	case n > 0 && n%60 == 0:
		return fmt.Sprintf(t("minutes"), n/60)
	}
	return fmt.Sprintf(t("seconds"), n)
}

// classify returns the S-pattern a statement matches, or "".
func (w *walker) classify(s ast.Stmt) string {
	switch s := s.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			switch types.ExprString(call.Fun) {
			case "assert.Pre":
				return kPre
			case "assert.Post":
				return kPost
			}
		}
	case *ast.IfStmt:
		if s.Init != nil || s.Else != nil || len(s.Body.List) != 1 {
			return ""
		}
		ret, ok := s.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 2 || !isEmptyOutput(ret.Results[0]) {
			return ""
		}
		id, ok := ret.Results[1].(*ast.Ident)
		switch {
		case !ok:
			return ""
		case id.Name == "err" && types.ExprString(s.Cond) == "err != nil":
			return kErrRet
		case w.fids[id.Name] != nil:
			return kGuard
		}
	case *ast.AssignStmt:
		if s.Tok != token.DEFINE || len(s.Rhs) != 1 {
			return ""
		}
		if len(s.Lhs) == 2 && types.ExprString(s.Lhs[1]) == "err" {
			if call, ok := s.Rhs[0].(*ast.CallExpr); ok && strings.HasPrefix(types.ExprString(call.Fun), "a.q.") {
				return kQuery
			}
		}
		if len(s.Lhs) == 1 {
			if call, ok := s.Rhs[0].(*ast.CallExpr); ok && types.ExprString(call.Fun) == "page.NextAfter" && w.pkgs["page"] {
				return kNext
			}
			return kLet
		}
	case *ast.RangeStmt:
		return kMapEach
	case *ast.ReturnStmt:
		if len(s.Results) == 2 && types.ExprString(s.Results[1]) == "nil" {
			if id, ok := s.Results[0].(*ast.Ident); ok && w.locals[id.Name] != nil {
				return kReturn
			}
		}
	}
	return ""
}

func isEmptyOutput(e ast.Expr) bool {
	lit, ok := e.(*ast.CompositeLit)
	return ok && types.ExprString(lit.Type) == "Output" && len(lit.Elts) == 0
}

func (w *walker) addStep(kind, text string) *Step {
	s := &Step{N: len(w.f.Steps) + 1, Text: text, kind: kind}
	w.f.Steps = append(w.f.Steps, s)
	return s
}

// notes adds what a reader must know about a step that may stop the action:
// assertions inside domain calls and what happens to the earlier writes.
func (w *walker) notes(s *Step, stops bool) {
	if w.domainNote(s) || stops {
		w.rolledBack(s, w.wrote)
	}
}

// domainNote states the assertions of domain functions called in this statement.
func (w *walker) domainNote(s *Step) bool {
	if len(w.asserts) == 0 {
		return false
	}
	quoted := make([]string, len(w.asserts))
	for i, m := range w.asserts {
		quoted[i] = fmt.Sprintf(st("quoted"), m)
	}
	note := fmt.Sprintf(st("domain asserts"), joinList(quoted), w.internal())
	for _, n := range s.Notes {
		if n == note {
			return true
		}
	}
	s.Notes = append(s.Notes, note)
	w.f.assertSteps = appendInt(w.f.assertSteps, s.N)
	return true
}

// What a write step may have changed by the time a later step stops the
// action. Every query runs in the call's one transaction (TxRule) and a step
// that stops the action rolls it back, so the English says, per earlier
// write, whether a change is rolled back, may be, or was never made:
//
//   - wroteSome: it changed rows. An insert (Q3) that succeeded always did;
//     a claim (Q6) did once a guard has stopped unless exactly one row (or
//     unless any row) changed. "The write in step N is rolled back."
//   - wroteMaybe: a claim whose changed-row count is not known at this step,
//     e.g. a read right after it, or the S10 guard "not exactly one" itself
//     (0 rows, or several). "Any change made in step N is rolled back."
//   - wroteNone: a claim that changed no row: only at a guard whose condition
//     includes "no <row> was changed in step N" (`n == 0 && ...`). Nothing
//     is rolled back, so the English says "Nothing was written in step N."
const (
	wroteNone  = "none"
	wroteMaybe = "maybe"
	wroteSome  = "some"
)

// The failure index ranks a failure case by the worst state of the writes
// before any of its steps.
const (
	whenBefore = iota // no write before it
	whenNone          // only writes that changed nothing
	whenMaybe         // a write that may have changed rows
	whenSome          // a write that changed rows
)

var whenOf = map[string]int{wroteNone: whenNone, wroteMaybe: whenMaybe, wroteSome: whenSome}

// whenAt is the worst write state in states (whenBefore without writes).
func (w *walker) whenAt(states map[int]string) int {
	when := whenBefore
	for _, n := range w.writes {
		if v := whenOf[states[n]]; v > when {
			when = v
		}
	}
	return when
}

// rolledBack states, for a step that may stop the action, what happens to
// each earlier write (see wroteSome, wroteMaybe, wroteNone).
func (w *walker) rolledBack(s *Step, states map[int]string) {
	var some, maybe, none []int
	for _, n := range w.writes {
		switch states[n] {
		case wroteSome:
			some = append(some, n)
		case wroteMaybe:
			maybe = append(maybe, n)
		case wroteNone:
			none = append(none, n)
		}
	}
	for _, g := range []struct {
		steps     []int
		one, many string
	}{
		{some, "rolled back 1", "rolled back n"},
		{maybe, "maybe rolled back 1", "maybe rolled back n"},
		{none, "nothing written 1", "nothing written n"},
	} {
		switch len(g.steps) {
		case 0:
		case 1:
			s.Notes = append(s.Notes, fmt.Sprintf(st(g.one), g.steps[0]))
		default:
			s.Notes = append(s.Notes, fmt.Sprintf(st(g.many), joinInts(g.steps)))
		}
	}
}

// conjuncts splits a condition at its top-level && (through parentheses).
func conjuncts(e ast.Expr) []ast.Expr {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return conjuncts(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.LAND {
			return append(conjuncts(x.X), conjuncts(x.Y)...)
		}
	}
	return []ast.Expr{e}
}

// changedCheck matches <n> <op> <int> on a claim's changed-row count (S10).
func (w *walker) changedCheck(e ast.Expr) (*local, token.Token, string) {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	b, ok := e.(*ast.BinaryExpr)
	if !ok {
		return nil, 0, ""
	}
	id, ok1 := b.X.(*ast.Ident)
	lit, ok2 := b.Y.(*ast.BasicLit)
	if !ok1 || !ok2 || lit.Kind != token.INT {
		return nil, 0, ""
	}
	loc := w.locals[id.Name]
	if loc == nil || loc.kind != "changed" {
		return nil, 0, ""
	}
	return loc, b.Op, lit.Value
}

// stopStates is what the writes changed when guard condition cond is true:
// a conjunct "<n> == 0" means that claim changed no row, "<n> == 1" that it
// changed one. Anything else (|| , !, "<n> != 1") tells nothing new.
func (w *walker) stopStates(cond ast.Expr) map[int]string {
	states := make(map[int]string, len(w.wrote))
	for k, v := range w.wrote {
		states[k] = v
	}
	for _, c := range conjuncts(cond) {
		loc, op, v := w.changedCheck(c)
		switch {
		case loc == nil:
		case op == token.EQL && v == "0":
			states[loc.step] = wroteNone
		case op == token.EQL && v == "1":
			states[loc.step] = wroteSome
		}
	}
	return states
}

// passGuard records what is known once guard condition cond was false: after
// "if <n> != 1 { stop }" exactly one row changed, after
// "if <n> != int64(len(in.<List>)) { stop }" one per entry, after
// "if <n> == 0 { stop }" at least one did.
func (w *walker) passGuard(cond ast.Expr) {
	if loc := w.isClaimCheck(cond); loc != nil {
		w.wrote[loc.step] = wroteSome // S10: exactly one row; S11: one row per entry of the list (at least one, D10)
		return
	}
	loc, op, v := w.changedCheck(cond)
	if loc != nil && op == token.EQL && v == "0" {
		w.wrote[loc.step] = wroteSome
	}
}

func appendInt(list []int, n int) []int {
	if len(list) > 0 && list[len(list)-1] == n {
		return list
	}
	return append(list, n)
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return joinList(parts)
}

func (w *walker) assertion(s ast.Stmt) string {
	call := s.(*ast.ExprStmt).X.(*ast.CallExpr)
	if len(call.Args) != 2 {
		w.refuse(call, "assertion with "+fmt.Sprint(len(call.Args))+" arguments", "S1/S6 assertion", `Write assert.Pre(<cond>, "<reason>")`)
		return "?"
	}
	msg, ok := stringLit(call.Args[1])
	if !ok || msg == "" {
		w.refuse(call.Args[1], "assertion reason that is not a string literal", "S1/S6 assertion", "Give a plain-English reason")
	}
	return fmt.Sprintf(st("assertion"), w.cond(call.Args[0]), msg)
}

func (w *walker) precondition(s ast.Stmt) {
	call := s.(*ast.ExprStmt).X.(*ast.CallExpr)
	if len(call.Args) > 0 {
		reads := false
		ast.Inspect(call.Args[0], func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "in" {
				reads = true
			}
			return !reads
		})
		if reads {
			w.refuse(call.Args[0], "precondition that reads the request (in)", "S1 precondition", meaningfulPrecondition)
		}
	}
	w.f.Pre = append(w.f.Pre, w.assertion(s))
}

func (w *walker) post(s ast.Stmt) {
	text := w.assertion(s)
	var step *Step
	if n := len(w.f.Steps); n > 0 && w.f.Steps[n-1].kind == "post" {
		step = w.f.Steps[n-1]
	} else {
		step = w.addStep("post", st("post"))
		step.Notes = append(step.Notes, fmt.Sprintf(st("post note"), w.internal()))
		w.rolledBack(step, w.wrote)
		w.f.assertSteps = appendInt(w.f.assertSteps, step.N)
	}
	step.Bullets = append(step.Bullets, text)
	w.domainNote(step)
}

func (w *walker) guard(s *ast.IfStmt) {
	id := s.Body.List[0].(*ast.ReturnStmt).Results[1].(*ast.Ident).Name
	fc := w.fids[id]
	cond := w.cond(s.Cond)
	if loc := w.isClaimCheck(s.Cond); loc != nil {
		loc.checked = true // S10/S11: the guard's entire condition is the check
	} else {
		w.markNested(s.Cond)
	}
	step := w.addStep("guard", fmt.Sprintf(st("guard"), cond, fc.ID, statusPhrase(fc.Status), fc.Message))
	fc.steps = append(fc.steps, step.N)
	stop := w.stopStates(s.Cond)
	if when := w.whenAt(stop); when > fc.when {
		fc.when = when
	}
	if hasCallTo(s.Cond, pageLimitCall) {
		w.f.pageLimitGuarded = true
	}
	w.domainNote(step)
	w.rolledBack(step, stop)
	w.passGuard(s.Cond)
}

// markNested remembers, for the refusal of a claim no guard checks, a guard
// that tests its check inside a compound condition.
func (w *walker) markNested(cond ast.Expr) {
	ast.Inspect(cond, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if loc := w.isClaimCheck(e); loc != nil && loc.nested == 0 {
				loc.nested = w.fset.Position(e.Pos()).Line
			}
		}
		return true
	})
}

func (w *walker) query(s *ast.AssignStmt) {
	call := s.Rhs[0].(*ast.CallExpr)
	w.sliceField = ""
	name := strings.TrimPrefix(types.ExprString(call.Fun), "a.q.")
	result := w.define(s.Lhs[0])
	if loc := w.locals[result]; loc != nil {
		loc.kind = "unknown" // until the query renders; avoids cascading refusals
	}
	if len(call.Args) == 0 || types.ExprString(call.Args[0]) != "ctx" {
		w.refuse(call, "query call without ctx first", "S3 query", "Pass ctx as the first argument")
		return
	}
	q := w.env.queries[name]
	if q == nil {
		w.refuse(s, "query "+name+" with no queries/*.sql definition", "S3 query",
			"Add -- name: "+name+" :one to a file in queries/ and run sqlc generate")
		return
	}
	w.f.Queries = append(w.f.Queries, &QueryCall{Name: name, pos: w.fset.Position(s.Pos())})
	if q.bad {
		return // already refused in queries/*.sql
	}
	vals := w.paramValues(call, q)
	if vals == nil {
		return
	}
	clock := false // Q1/Q2: a comparison with the current time (T1)
	for _, c := range q.Where {
		if !c.clockCond() {
			continue
		}
		clock = true
		if arg := w.args[c.Val.Param]; arg != nil && !w.isClockInput(arg) {
			ctx := "Q1 count"
			if q.Shape == "row" {
				ctx = "Q2 one row"
			}
			w.refuse(arg, "comparison "+c.Col+" "+c.Op+" "+sqlParamText(c.Val)+" in query "+q.Name+", whose value "+types.ExprString(arg)+" is not the server-set current time",
				ctx, readClockHint)
		}
	}
	where := func(tmpl string) string {
		parts := make([]string, len(q.Where))
		for i, c := range q.Where {
			key := tmpl
			switch {
			case c.Val.Kind == "slice":
				key += " in" // Q7: "`id` is one of the request's `seat_ids`"
			case c.clockCond() && c.Val.Op != "":
				n, _ := strconv.Atoi(c.Val.Off)
				parts[i] = clockComparison(c.Col, c.Op, c.Val.Op, n) // the Q6 boundary words
				continue
			case c.clockCond():
				parts[i] = clockNow("`"+c.Col+"`", c.Op) // the Q6 boundary words
				continue
			}
			parts[i] = fmt.Sprintf(t(key), c.Col, w.sqlValue(c.Val, vals, c.Col, q.Table))
		}
		return strings.Join(parts, " and ")
	}
	loc := w.locals[result]
	var text, fails string
	switch q.Shape {
	case "count":
		*loc = local{kind: "count", table: q.Table, where: where("where is"), has: where("where has"), whose: clock}
		text, fails = fmt.Sprintf(st("count"), plural(q.Table), loc.where, q.Name, q.File), fmt.Sprintf(st("query fails"), w.internal())
	case "row":
		*loc = local{kind: "row", table: q.Table, phrase: fmt.Sprintf(t("found row"), singular(q.Table)), cols: q.Cols}
		text, fails = fmt.Sprintf(st("row"), singular(q.Table), where("where is"), q.Name, q.File, singular(q.Table)),
			fmt.Sprintf(st("row fails"), singular(q.Table), w.internal())
	case "page":
		*loc = local{kind: "rows", table: q.Table, phrase: fmt.Sprintf(t("listed rows"), plural(q.Table)), cols: q.Cols}
		limitEn := w.sqlValue(q.Limit, vals, "limit", q.Table)
		cursorEn := w.sqlValue(q.CursorVal, vals, q.CursorCol, q.Table)
		loc.limit = limitEn
		text, fails = fmt.Sprintf(st("page"), plural(q.Table), where("where is"), q.CursorCol, cursorEn, q.CursorCol, limitEn, q.Name, q.File, plural(q.Table)),
			fmt.Sprintf(st("query fails"), w.internal())
		w.f.hasPageQuery = true
		if max, _ := pageSizes(); q.Limit.Kind == "int" && q.LimitN > max {
			w.refuse(s, fmt.Sprintf("query %s with LIMIT %d", q.Name, q.LimitN), "Q5 keyset page", fmt.Sprintf("LIMIT must be 1..%d (page.MaxPageSize)", max))
		}
	case "insert":
		*loc = local{kind: "row", table: q.Table, phrase: fmt.Sprintf(t("new row"), singular(q.Table)), cols: q.Cols}
		assigns := make([]string, len(q.Values))
		for i, v := range q.Values {
			assigns[i] = fmt.Sprintf(t("field"), v.Col, w.sqlValue(v.Val, vals, v.Col, q.Table))
		}
		text, fails = fmt.Sprintf(st("insert"), singular(q.Table), q.Table, joinList(assigns), q.Name, q.File, singular(q.Table)),
			fmt.Sprintf(st("insert fails"), w.internal())
	case "claim":
		*loc = local{kind: "changed", table: q.Table, stmt: s, multi: q.Slice != "", list: w.sliceField}
		sets := make([]string, len(q.Values))
		for i, v := range q.Values {
			sets[i] = fmt.Sprintf(t("field"), v.Col, w.sqlValue(v.Val, vals, v.Col, q.Table))
		}
		conds := make([]string, len(q.Conds))
		for i, c := range q.Conds {
			conds[i] = w.claimCond(c, vals, q.Table)
		}
		text, fails = fmt.Sprintf(st("claim"), q.Table, joinList(sets), singular(q.Table), strings.Join(conds, " and "), q.Name, q.File, singular(q.Table)),
			fmt.Sprintf(st("query fails"), w.internal())
	}
	if own := w.ownership(s, q); own != "" { // A4: said between what the step does and how it fails
		text += " " + own
	}
	text += " " + fails
	// W1: a write to a table this action already read is check-then-write.
	for _, wr := range q.Writes {
		if at, ok := w.readAt[wr]; ok {
			w.refuse(s, fmt.Sprintf("write to table %s after reading it in step %d (check-then-write)", wr, at), "W1 no check-then-write", checkThenWriteHint)
		}
	}
	if loc.kind == "row" {
		for n, other := range w.locals {
			if n != result && other.kind == "row" && other.phrase == loc.phrase {
				w.refuse(s, "second query result called the "+loc.phrase, "S3 query", "bridge-en names a row by its table; read or write each table once per action")
			}
		}
	}
	step := w.addStep("query", text)
	if loc != nil {
		loc.step = step.N
	}
	w.notes(step, true)
	w.f.queryFailSteps = append(w.f.queryFailSteps, step.N)
	if len(q.Writes) == 0 {
		for _, r := range q.Reads {
			if _, ok := w.readAt[r]; !ok {
				w.readAt[r] = step.N
			}
		}
	}
	for _, r := range q.Reads {
		w.f.reads = appendUnique(w.f.reads, r)
	}
	for _, wr := range q.Writes {
		w.f.writes = appendUnique(w.f.writes, wr)
	}
	if len(q.Writes) > 0 {
		w.writes = append(w.writes, step.N)
		w.wrote[step.N] = wroteSome // an insert that succeeded stored its row
		if q.Shape == "claim" {
			w.wrote[step.N] = wroteMaybe // until a guard checks how many rows changed (S10)
		}
		if w.f.Method == "GET" {
			w.refuse(s, "write query "+q.Name+" in a GET action", "S3 query", "A GET runs in a read-only transaction (ReadTxRule); writes belong in a POST action")
		}
	}
}

// paramValues maps each SQL parameter of q to the rendered Go value passed for it.
func (w *walker) paramValues(call *ast.CallExpr, q *SQLQuery) map[string]string {
	args := call.Args[1:]
	vals := map[string]string{}
	w.args = map[string]ast.Expr{}
	var lit *ast.CompositeLit
	if len(args) == 1 {
		switch a := args[0].(type) {
		case *ast.CompositeLit:
			lit = a
		case *ast.Ident:
			if loc := w.locals[a.Name]; loc != nil && loc.lit != nil {
				lit = loc.lit
			}
		}
	}
	if lit != nil {
		typ := types.ExprString(lit.Type)
		if typ != "db."+q.Name+"Params" {
			w.refuse(lit, "record "+typ+" passed to query "+q.Name, "S3 query", "Pass db."+q.Name+"Params")
			return nil
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				w.refuse(el, "unkeyed record field", "E6 record", "Write Field: value")
				return nil
			}
			key := types.ExprString(kv.Key)
			p := matchName(q.Params, key)
			if p == "" {
				w.refuse(kv, "field "+key+" of "+typ+", which matches no parameter of query "+q.Name, "S3 query", "")
				return nil
			}
			vals[p], w.args[p] = w.paramValue(q, p, kv.Value), kv.Value
		}
		for _, p := range q.Params {
			if _, ok := vals[p]; !ok {
				w.refuse(lit, "query "+q.Name+" parameter "+p+" that "+typ+" does not set", "S3 query", "Set every parameter by name")
				return nil
			}
		}
		return vals
	}
	if len(args) != len(q.Params) {
		w.refuse(call, fmt.Sprintf("query %s called with %d values for %d SQL parameters", q.Name, len(args), len(q.Params)), "S3 query", "")
		return nil
	}
	for i, a := range args {
		vals[q.Params[i]], w.args[q.Params[i]] = w.paramValue(q, q.Params[i], a), a
	}
	return vals
}

// paramValue renders the Go value passed for SQL parameter p of q. The list
// of a Q7 IN (sqlc.slice(p)) takes exactly a D10 list input, in.<List>;
// every other parameter takes a value (and never a list).
func (w *walker) paramValue(q *SQLQuery, p string, e ast.Expr) string {
	if q == nil || q.Slice != p {
		return w.value(e)
	}
	name, json, ok := w.listInput(e)
	if !ok {
		w.refuse(e, "value "+types.ExprString(e)+" for IN (sqlc.slice("+p+")) that is not a list input", "Q7 IN list",
			"Pass the request's list field (D10) itself, for example "+goName(p)+": in.SeatIDs")
		return "?"
	}
	w.sliceField = name
	return fmt.Sprintf(t("request field"), json)
}

// isClockInput reports whether e is exactly in.<Field> for the Input field
// tagged clock:"now" (T1): the only value a Q1/Q2 comparison with the
// current time may be bound to, so "the current time" in the English is true.
func (w *walker) isClockInput(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	root, ok := sel.X.(*ast.Ident)
	if !ok || w.locals[root.Name] == nil || w.locals[root.Name].kind != "request" {
		return false
	}
	for _, f := range w.f.Input {
		if f.Name == sel.Sel.Name && f.ServerSet == "clock" {
			return true
		}
	}
	return false
}

// sqlParamText writes a parameter value back as SQL for a refusal:
// sqlc.arg(now) - 600.
func sqlParamText(v sqlVal) string {
	s := "sqlc.arg(" + v.Param + ")"
	if v.Op != "" {
		s += " " + v.Op + " " + v.Off
	}
	return s
}

// goName is the Go field name sqlc gives parameter p (seat_ids -> SeatIds).
func goName(p string) string {
	parts := strings.Split(p, "_")
	for i, s := range parts {
		if s != "" {
			parts[i] = strings.ToUpper(s[:1]) + s[1:]
		}
	}
	return strings.Join(parts, "")
}

// matchName finds the SQL name that a Go field name stands for (CustomerID ~ customer_id).
func matchName(sqlNames []string, goName string) string {
	for _, n := range sqlNames {
		if norm(n) == norm(goName) {
			return n
		}
	}
	return ""
}

func (w *walker) sqlValue(v sqlVal, vals map[string]string, col, table string) string {
	switch v.Kind {
	case "param":
		if v.Op != "" {
			return offsetPhrase(vals[v.Param], v.Op, v.Off)
		}
		return vals[v.Param]
	case "int":
		return fmt.Sprintf(t("int"), v.Lit)
	case "string":
		text := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(v.Lit, "'"), "'"), "''", "'")
		return fmt.Sprintf(t("string"), strconv.Quote(text))
	case "next":
		return fmt.Sprintf(t("next number"), col, table)
	case "slice":
		return vals[v.Param]
	}
	return "?"
}

func (w *walker) let(s *ast.AssignStmt) {
	name := w.define(s.Lhs[0])
	loc := w.locals[name]
	lit, ok := s.Rhs[0].(*ast.CompositeLit)
	if !ok {
		step := w.addStep("let", fmt.Sprintf(st("let"), name, w.value(s.Rhs[0])))
		w.notes(step, false)
		return
	}
	typ := w.recordType(lit)
	switch {
	case typ == "":
		return
	case typ == "Output":
		for n, other := range w.locals {
			if n != name && other.kind == "answer" {
				w.refuse(lit, "second Output record", "S5 let", "Build the answer once")
			}
		}
		loc.kind = "answer"
		step := w.addStep("answer", st("answer"))
		step.Bullets = w.outputFields(lit)
		w.notes(step, false)
	case strings.HasPrefix(typ, "domain."):
		loc.typ = strings.TrimPrefix(typ, "domain.")
		phrase := w.domainTypePhrase(lit, loc.typ, false)
		step := w.addStep("let", fmt.Sprintf(st("let record"), name, phrase))
		step.Bullets = w.domainFields(lit, loc.typ)
		w.notes(step, false)
	default: // db.<Query>Params, passed to a query later
		loc.lit = lit
		query := strings.TrimSuffix(strings.TrimPrefix(typ, "db."), "Params")
		var params []string
		if q := w.env.queries[query]; q != nil {
			params = q.Params
		}
		step := w.addStep("let", fmt.Sprintf(st("let record"), name, fmt.Sprintf(st("params"), query)))
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				w.refuse(el, "unkeyed record field", "E6 record", "Write Field: value")
				continue
			}
			key := types.ExprString(kv.Key)
			col := matchName(params, key)
			if col == "" {
				col = key
			}
			step.Bullets = append(step.Bullets, fmt.Sprintf(t("field"), col, w.paramValue(w.env.queries[query], col, kv.Value)))
		}
		w.notes(step, false)
	}
}

func (w *walker) outputFields(lit *ast.CompositeLit) []string {
	var out []string
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			w.refuse(el, "unkeyed record field", "E6 record", "Write Field: value")
			continue
		}
		key := types.ExprString(kv.Key)
		json := key
		for _, f := range w.f.Output {
			if f.Name == key {
				json = f.JSON
			}
		}
		if id, ok := kv.Value.(*ast.Ident); ok && w.locals[id.Name] != nil && w.locals[id.Name].kind == "cursor" {
			w.f.nextField = json
		}
		out = append(out, fmt.Sprintf(t("field"), json, w.value(kv.Value)))
	}
	return out
}

func (w *walker) ret(s *ast.ReturnStmt) {
	id := s.Results[0].(*ast.Ident)
	what := w.value(id)
	code, ok := w.env.http.Success[w.f.Method]
	if !ok {
		w.refuse(s, "route method "+w.f.Method+" without an httpx.SuccessStatus entry", "H1 http plumbing", "runtime/httpx answers only the methods in SuccessStatus")
		return
	}
	if len(w.f.Queries) == 0 {
		w.addStep("return", fmt.Sprintf(st("return"), statusPhrase(code), what))
		return
	}
	tmpl := "commit return"
	if w.f.Method == "GET" {
		tmpl = "read return"
	}
	step := w.addStep("return", fmt.Sprintf(st(tmpl), statusPhrase(code), what, w.internal()))
	w.f.commitStep = step.N
}

func (w *walker) define(e ast.Expr) string {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		w.refuse(e, "assignment to "+types.ExprString(e), "S3/S5", "Bind a new, named local")
		return "?"
	}
	if w.locals[id.Name] != nil {
		w.refuse(e, "redefinition of "+id.Name, "S3/S5", "Every local is defined once")
	}
	w.locals[id.Name] = &local{kind: "let"}
	return id.Name
}

// finish checks the F-IDs and fills the contract sentences.
func (w *walker) finish() {
	f, h := w.f, w.env.http
	sort.Slice(f.Failures, func(i, j int) bool { return fidNum(f.Failures[i].ID) < fidNum(f.Failures[j].ID) })
	byStatus := map[int][]string{}
	var statuses []int
	for _, fc := range f.Failures {
		if len(fc.steps) == 0 {
			w.errs = append(w.errs, Refusal{Pos: fc.pos, Construct: "failure case " + fc.ID + " that no guard raises",
				Context: "D6 failures", Hint: "Every declared F-ID needs an S2 guard: if <cond> { return Output{}, " + fc.ID + " }"})
			continue
		}
		where := fmt.Sprintf(docSentences["step"], fc.steps[0])
		if len(fc.steps) > 1 {
			where = fmt.Sprintf(docSentences["steps"], joinInts(fc.steps))
		}
		when := docSentences[[]string{"before write", "after no write", "after maybe write", "after write"}[fc.when]]
		fc.Line = fmt.Sprintf(docSentences["failure index"], fc.ID, fc.Message, statusPhrase(fc.Status), where, when)
		if w.f.Method == "GET" {
			fc.Line = fmt.Sprintf(docSentences["read index"], fc.ID, fc.Message, statusPhrase(fc.Status), where)
		}
		if byStatus[fc.Status] == nil {
			statuses = append(statuses, fc.Status)
		}
		byStatus[fc.Status] = append(byStatus[fc.Status], fc.ID)
	}
	count := func(n int) string {
		if n == 1 {
			return docSentences["fields one"]
		}
		return fmt.Sprintf(docSentences["fields many"], n)
	}
	signedOut := map[bool]string{true: h.SignedOutRule}[f.Public] // T3: a Public action may run signed out
	if f.Public {
		f.AccessLine = h.PublicRule
	} else if len(f.Roles) > 0 {
		names := make([]string, len(f.Roles))
		for i, r := range f.Roles {
			names[i] = "`" + r + "`"
		}
		f.AccessLine = strings.Replace(h.RolesRule, "{roles}", joinOr(names), 1)
	}
	for i := range f.Input {
		in := &f.Input[i]
		rule := map[string]string{"user": h.UserRule, "role": h.RoleRule}[in.ServerSet]
		if rule == "" {
			continue
		}
		out := strings.Replace(signedOut, "{zero}", h.SignedOutZero[in.goType], 1)
		in.Line = fmt.Sprintf(docSentences["field"], in.JSON, strings.Replace(rule, "{signed out}", out, 1))
	}
	for _, in := range f.Input {
		if in.ServerSet != "" {
			f.ServerSet = append(f.ServerSet, in)
		} else {
			f.BodyInput = append(f.BodyInput, in)
		}
	}
	switch n := len(f.ServerSet); {
	case n == 1:
		f.ServerIntro = docSentences["server intro 1"]
	case n > 1:
		f.ServerIntro = fmt.Sprintf(docSentences["server intro n"], n)
	}
	f.InputCount = count(len(f.BodyInput))
	if f.Method == "GET" && len(f.BodyInput) == 0 {
		f.InputIntro = docSentences["get no input"] // only server-set values (T1-T3)
		f.InputRule = h.StrictQueryRule             // G10
	} else if f.Method == "GET" {
		f.InputIntro = docSentences["get input"]
		f.InputRule = h.QueryInputRule
		if f.InputRule == "" || h.StrictQueryRule == "" {
			w.errs = append(w.errs, Refusal{Pos: token.Position{Filename: "runtime/httpx"}, Construct: "missing QueryInputRule or StrictQueryRule", Context: "H1 http plumbing", Hint: "runtime/httpx declares QueryInputRule and StrictQueryRule for GET slices"})
		}
		f.InputRule += " " + h.StrictQueryRule // G10
	} else {
		f.InputIntro = "The request body is one JSON object with " + f.InputCount + " and no others:"
		f.InputRule = h.InputRule
	}
	txRule := h.TxRule
	if f.Method == "GET" {
		txRule = h.ReadTxRule
	}
	if f.commitStep > 0 && len(f.queryFailSteps) > 0 {
		f.TxLine = strings.NewReplacer(
			"{first}", strconv.Itoa(f.queryFailSteps[0]),
			"{last}", strconv.Itoa(f.commitStep-1),
			"{commit}", strconv.Itoa(f.commitStep),
			"{end}", strconv.Itoa(f.commitStep),
		).Replace(txRule)
	}
	f.ErrorShape = h.ErrorShape
	f.InternalStatus = w.internal()
	f.NoPre = docSentences["no pre"]
	if code, ok := h.Success[f.Method]; ok {
		f.SuccessLine = fmt.Sprintf(docSentences["success"], statusPhrase(code), count(len(f.Output)))
	}
	msg := func(o outcome) string {
		if o.Message == "" {
			return docSentences["message varies"]
		}
		return fmt.Sprintf(docSentences["message fixed"], o.Message)
	}
	line := func(o outcome, when string) string {
		s := fmt.Sprintf(docSentences["outcome"], statusPhrase(o.Status), o.ID, msg(o), when)
		if o.Note != "" {
			s += " " + o.Note
		}
		return s
	}
	badWhen := h.BadInput.When
	if f.Method == "GET" && h.BadQueryWhen != "" {
		badWhen = h.BadQueryWhen
	}
	if hasList(f.Input) {
		badWhen = fmt.Sprintf(docSentences["server set when"], badWhen, h.ListWhen) // D10: ListWhen
	}
	if len(f.ServerSet) > 0 {
		badWhen = fmt.Sprintf(docSentences["server set when"], badWhen, h.ServerSetWhen)
	}
	f.Answers = append(f.Answers, line(h.BadInput, badWhen))
	if !f.Public && len(f.Roles) > 0 { // A3: Bind answers these before it reads the request
		f.Answers = append(f.Answers, line(h.Unauthenticated, h.Unauthenticated.When), line(h.Forbidden, h.Forbidden.When))
	}
	sort.Ints(statuses)
	for _, code := range statuses {
		f.Answers = append(f.Answers, fmt.Sprintf(docSentences["failure status"], statusPhrase(code), joinOr(byStatus[code])))
	}
	var causes []string
	if len(f.queryFailSteps) > 0 {
		causes = append(causes, fmt.Sprintf(docSentences["query fails at"], stepsPhrase(f.queryFailSteps, false)))
	}
	if len(f.assertSteps) > 0 || len(f.Pre) > 0 {
		causes = append(causes, fmt.Sprintf(docSentences["assert fails at"], stepsPhrase(f.assertSteps, len(f.Pre) > 0)))
	}
	if f.commitStep > 0 {
		ends := "commit fails at"
		if f.Method == "GET" {
			ends = "end fails at"
		}
		causes = append(causes, fmt.Sprintf(docSentences[ends], stepsPhrase([]int{f.commitStep}, false)))
	}
	f.Answers = append(f.Answers, line(h.Internal, joinOr(causes)))
	var data []string
	if len(f.reads) > 0 {
		sort.Strings(f.reads)
		data = append(data, fmt.Sprintf(docSentences["reads"], tables(f.reads)))
	}
	if len(f.writes) > 0 {
		sort.Strings(f.writes)
		data = append(data, fmt.Sprintf(docSentences["writes"], tables(f.writes)))
	}
	if len(data) == 0 {
		f.DataLine = docSentences["no data"]
	} else {
		f.DataLine = fmt.Sprintf(docSentences["data"], joinList(data))
		if f.Method == "GET" {
			f.DataLine = fmt.Sprintf(docSentences["read data"], joinList(data))
		}
	}
	if f.hasPageQuery && !f.pageLimitGuarded {
		w.errs = append(w.errs, Refusal{Pos: token.Position{Filename: "action.go"}, Construct: "keyset page query without a page.IsPageLimit guard",
			Context: "Q5 keyset page", Hint: "Guard the request's limit with page.IsPageLimit (1..page.MaxPageSize) before the list query"})
	}
	if max, def := pageSizes(); f.hasPageQuery {
		contract := fmt.Sprintf(docSentences["page contract"], max, def)
		if f.nextField != "" {
			contract = fmt.Sprintf(docSentences["page next"], f.nextField, max, def)
		}
		f.Answers = append([]string{contract}, f.Answers...)
	}
}

func hasList(fields []Field) bool {
	for _, f := range fields {
		if f.List != "" {
			return true
		}
	}
	return false
}

func stepsPhrase(steps []int, beforeFirst bool) string {
	var parts []string
	if beforeFirst {
		parts = append(parts, docSentences["before step 1"])
	}
	switch len(steps) {
	case 0:
	case 1:
		parts = append(parts, fmt.Sprintf(docSentences["step"], steps[0]))
	default:
		parts = append(parts, fmt.Sprintf(docSentences["steps"], joinInts(steps)))
	}
	return joinList(parts)
}

func joinOr(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
}

func isMakeLenAssign(s ast.Stmt) bool {
	as, ok := s.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok || types.ExprString(call.Fun) != "make" || len(call.Args) != 2 {
		return false
	}
	if _, ok := call.Args[0].(*ast.ArrayType); !ok {
		return false
	}
	lenCall, ok := call.Args[1].(*ast.CallExpr)
	return ok && types.ExprString(lenCall.Fun) == "len" && len(lenCall.Args) == 1
}

func hasCallTo(e ast.Expr, fun string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if types.ExprString(call.Fun) == fun {
			found = true
		}
		return !found
	})
	return found
}

// mapEach renders S8: make + for-range that builds []domain.T from a Q5 result.
func (w *walker) mapEach(makeAssign *ast.AssignStmt, rs *ast.RangeStmt) {
	items := w.define(makeAssign.Lhs[0])
	call := makeAssign.Rhs[0].(*ast.CallExpr)
	arr := call.Args[0].(*ast.ArrayType)
	lenCall := call.Args[1].(*ast.CallExpr)
	rowsName := ""
	if id, ok := lenCall.Args[0].(*ast.Ident); ok {
		rowsName = id.Name
	}
	rows := w.locals[rowsName]
	if rows == nil || rows.kind != "rows" {
		w.refuse(lenCall.Args[0], "map-each over something that is not a Q5 list result", "S8 map each",
			"Range over the local bound by a Q5 :many query")
		return
	}
	elt := types.ExprString(arr.Elt)
	if !strings.HasPrefix(elt, "domain.") {
		w.refuse(arr.Elt, "map-each element type "+elt, "S8 map each", "Use []domain.<T>")
		return
	}
	domType := strings.TrimPrefix(elt, "domain.")
	phrase := w.domainTypePhrase(arr.Elt, domType, false)

	if rs.Key == nil || rs.Value == nil || rs.Tok != token.DEFINE {
		w.refuse(rs, "for-range of a different shape", "S8 map each", "Write: for i, row := range rows { items[i] = domain.T{...} }")
		return
	}
	key, ok1 := rs.Key.(*ast.Ident)
	val, ok2 := rs.Value.(*ast.Ident)
	x, ok3 := rs.X.(*ast.Ident)
	if !ok1 || !ok2 || !ok3 || x.Name != rowsName || key.Name == "_" || val.Name == "_" {
		w.refuse(rs, "for-range of a different shape", "S8 map each", "Write: for i, row := range rows { items[i] = domain.T{...} }")
		return
	}
	if len(rs.Body.List) != 1 {
		w.refuse(rs.Body, "map-each body with "+fmt.Sprint(len(rs.Body.List))+" statements", "S8 map each", "The body is one assignment: items[i] = domain.T{...}")
		return
	}
	assign, ok := rs.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		w.refuse(rs.Body.List[0], "map-each body that is not items[i] = domain.T{...}", "S8 map each", "")
		return
	}
	idx, ok := assign.Lhs[0].(*ast.IndexExpr)
	if !ok {
		w.refuse(assign.Lhs[0], "map-each assignment not to items[i]", "S8 map each", "")
		return
	}
	if types.ExprString(idx.X) != items || types.ExprString(idx.Index) != key.Name {
		w.refuse(idx, "map-each assignment not to "+items+"["+key.Name+"]", "S8 map each", "")
		return
	}
	lit, ok := assign.Rhs[0].(*ast.CompositeLit)
	if !ok || types.ExprString(lit.Type) != "domain."+domType {
		w.refuse(assign.Rhs[0], "map-each value that is not domain."+domType+"{...}", "S8 map each", "")
		return
	}

	// Bind range locals for field rendering inside the composite lit.
	w.locals[key.Name] = &local{kind: "index"}
	w.locals[val.Name] = &local{kind: "row", table: rows.table, phrase: fmt.Sprintf(t("listed row"), singular(rows.table)), cols: rows.cols}
	bullets := w.domainFields(lit, domType)
	delete(w.locals, key.Name)
	delete(w.locals, val.Name)

	w.locals[items].kind = "list"
	w.locals[items].phrase = phrase
	w.locals[items].typ = domType

	step := w.addStep("map each", fmt.Sprintf(st("map each"), plural(rows.table), phrase))
	w.locals[items].step = step.N
	step.Bullets = bullets
	step.Notes = append(step.Notes, fmt.Sprintf(st("map each empty"), plural(rows.table)))
	w.notes(step, false)
}

// stmtName names a refused statement for the error message.
func stmtName(s ast.Stmt) string {
	switch s := s.(type) {
	case *ast.ForStmt:
		return "for loop"
	case *ast.RangeStmt:
		return "range loop"
	case *ast.GoStmt:
		return "go statement (goroutine)"
	case *ast.DeferStmt:
		return "defer statement"
	case *ast.SwitchStmt:
		return "switch statement"
	case *ast.TypeSwitchStmt:
		return "type switch"
	case *ast.SelectStmt:
		return "select statement"
	case *ast.LabeledStmt:
		return "labeled statement"
	case *ast.BranchStmt:
		return s.Tok.String() + " statement"
	case *ast.IncDecStmt:
		return "increment/decrement (" + s.Tok.String() + ")"
	case *ast.SendStmt:
		return "channel send"
	case *ast.DeclStmt:
		return "local var/const/type declaration"
	case *ast.BlockStmt:
		return "bare block"
	case *ast.IfStmt:
		switch {
		case s.Else != nil:
			return "if/else statement"
		case s.Init != nil:
			return "if statement with an init clause"
		}
		return "if statement whose body is not a single failure return"
	case *ast.AssignStmt:
		if s.Tok == token.ASSIGN {
			return "reassignment (=)"
		}
		if s.Tok != token.DEFINE {
			return "compound assignment (" + s.Tok.String() + ")"
		}
		if len(s.Lhs) == 1 {
			return "short variable declaration (:=)"
		}
		return "multiple assignment"
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			return "call to " + types.ExprString(call.Fun) + " as a statement"
		}
		return "expression statement"
	case *ast.ReturnStmt:
		return "return statement of a different shape"
	}
	return fmt.Sprintf("statement %T", s)
}

// rbrace positions a refusal at the closing brace of a block.
type rbrace struct{ b *ast.BlockStmt }

func (r rbrace) Pos() token.Pos { return r.b.Rbrace }
func (r rbrace) End() token.Pos { return r.b.Rbrace + 1 }

// pageSizes are runtime/page's MaxPageSize and DefaultPageSize: the only
// source of the page limits (httpx and the English both use them).
func pageSizes() (max, def int) { return int(page.MaxPageSize), int(page.DefaultPageSize) }

// nextCursor renders S9: <name> := page.NextAfter(<rows>, "<cursor>", <limit>).
func (w *walker) nextCursor(s *ast.AssignStmt) {
	const ctx = "S9 next cursor"
	call := s.Rhs[0].(*ast.CallExpr)
	name := w.define(s.Lhs[0])
	hint := `Write: next := page.NextAfter(rows, "<cursor column>", <the page query's LIMIT value>)`
	if len(call.Args) != 3 || w.f.hasNext {
		w.refuse(call, "page.NextAfter of a different shape", ctx, hint+"; at most once")
		return
	}
	id, ok := call.Args[0].(*ast.Ident)
	rows := (*local)(nil)
	if ok {
		rows = w.locals[id.Name]
	}
	if rows == nil || rows.kind != "rows" {
		w.refuse(call.Args[0], "page.NextAfter over something that is not a Q5 list result", ctx, hint)
		return
	}
	col, ok := stringLit(call.Args[1])
	q := w.pageQuery()
	if !ok || q == nil || col != q.CursorCol {
		w.refuse(call.Args[1], "page.NextAfter key that is not the page query's cursor column", ctx, hint)
		return
	}
	if limit := w.value(call.Args[2]); limit != rows.limit {
		w.refuse(call.Args[2], "page.NextAfter limit "+limit+", which is not the page query's LIMIT ("+rows.limit+")", ctx, hint)
		return
	}
	w.locals[name].kind = "cursor"
	w.f.hasNext = true
	step := w.addStep("let", fmt.Sprintf(st("next cursor"), col, singular(rows.table), rows.limit, plural(rows.table)))
	w.notes(step, false)
}

// pageQuery is the slice's Q5 query, if it has one.
func (w *walker) pageQuery() *SQLQuery {
	for _, c := range w.f.Queries {
		if q := w.env.queries[c.Name]; q != nil && q.Shape == "page" {
			return q
		}
	}
	return nil
}
