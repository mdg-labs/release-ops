package tickettemplate

import (
	"fmt"
	"strings"
	"text/template"
	"text/template/parse"
)

// scope names the type the dot has at a point of a template.
type scope int

const (
	scopeRoot scope = iota
	scopePullRequest
	scopeDependency
	scopeSupersede
	// scopeOpaque is a dot whose type is not tracked (a range over something else): fields
	// are not checked inside it.
	scopeOpaque
	// scopeList is the Dependencies list and scopeValue a string or a number; neither has
	// fields.
	scopeList
	scopeValue
)

// fields lists the variables each scope offers (specs §5.4).
var fields = map[scope][]string{
	scopeRoot:        {"PullRequest", "Repository", "Dependencies", "ReleaseNotes", "Supersede"},
	scopePullRequest: {"URL", "Number", "Title"},
	scopeDependency:  {"Name", "From", "To", "UpdateType"},
	scopeSupersede:   {"NewPullRequest", "NewTicketURL"},
}

// replacements says what replaced a variable of the release-based contract, so the error of
// a template that still uses one names the variable to write instead.
var replacements = map[string]string{
	"Repo":             ".Repository",
	"Repo.SourceKind":  ".Repository",
	"Repo.ProjectPath": ".Repository",
	"Repo.URL":         ".Repository",
	"Release":          ".Dependencies and .ReleaseNotes",
	"Previous":         ".Dependencies",
	"Supersede.OldTag": ".PullRequest",
	"Supersede.NewTag": ".Supersede.NewPullRequest",
}

// frame holds the variables one lexical scope of a template declares: the scope of the value
// each is bound to. A template body, and the body of each if, with and range, is a frame whose
// parent is the one around it.
type frame struct {
	parent *frame
	vars   map[string]scope
}

func newFrame(parent *frame) *frame {
	return &frame{parent: parent, vars: map[string]scope{}}
}

func (f *frame) lookup(name string) scope {
	for fr := f; fr != nil; fr = fr.parent {
		if s, ok := fr.vars[name]; ok {
			return s
		}
	}
	return scopeOpaque
}

func (f *frame) declare(name string, s scope) { f.vars[name] = s }

// assign rebinds the variable where it was declared. An assignment inside a branch or a loop
// body may or may not run, so a value that differs from the declared one leaves the variable
// untracked.
func (f *frame) assign(name string, s scope) {
	for fr := f; fr != nil; fr = fr.parent {
		if old, ok := fr.vars[name]; ok {
			if fr != f && old != s {
				s = scopeOpaque
			}
			fr.vars[name] = s
			return
		}
	}
}

// env is what a point of a template can refer to: the dot, and the variables in reach, `$`
// among them.
type env struct {
	dot  scope
	vars *frame
	defs *definitions
}

func (e env) inner(dot scope) env { return env{dot: dot, vars: newFrame(e.vars), defs: e.defs} }

// definitions are the named templates a template can invoke. Each is checked once for every
// scope it is invoked with, which also ends a definition that invokes itself.
type definitions struct {
	t       *template.Template
	visited map[invocation]bool
}

type invocation struct {
	name string
	arg  scope
}

// checkVariables refuses a template that names a field the context does not have. Execution
// would fail on the same fields, but only on the branches it takes; walking the parse tree
// covers every branch when the template is saved.
func checkVariables(t *template.Template) error {
	if t.Tree == nil || t.Root == nil {
		return nil
	}
	root := newFrame(nil)
	root.declare("$", scopeRoot)
	defs := &definitions{t: t, visited: map[invocation]bool{{t.Name(), scopeRoot}: true}}
	return walk(t.Root, env{dot: scopeRoot, vars: root, defs: defs})
}

func walk(node parse.Node, e env) error {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return nil
		}
		for _, c := range n.Nodes {
			if err := walk(c, e); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return walkPipe(n.Pipe, e, true)
	case *parse.PipeNode:
		return walkPipe(n, e, true)
	case *parse.FieldNode:
		return checkField(n.Ident, e.dot)
	case *parse.VariableNode:
		return checkField(n.Ident[1:], e.vars.lookup(n.Ident[0]))
	case *parse.ChainNode:
		switch base := n.Node.(type) {
		case *parse.DotNode:
			return checkField(n.Field, e.dot)
		case *parse.PipeNode:
			if err := walk(base, e); err != nil {
				return err
			}
			return checkField(n.Field, pipeScope(base, e))
		default:
			return walk(n.Node, e)
		}
	case *parse.IfNode:
		return walkBranch(&n.BranchNode, e, e.dot, pipeScope(n.Pipe, e), false)
	case *parse.WithNode:
		inner := pipeScope(n.Pipe, e)
		return walkBranch(&n.BranchNode, e, inner, inner, false)
	case *parse.RangeNode:
		return walkBranch(&n.BranchNode, e, elementScope(pipeScope(n.Pipe, e)), pipeScope(n.Pipe, e), true)
	case *parse.TemplateNode:
		if err := walkPipe(n.Pipe, e, false); err != nil {
			return err
		}
		return walkDefinition(n, e)
	}
	return nil
}

// walkDefinition checks the body of the template an invocation names, with the argument of the
// invocation as its dot and as its `$`. An invocation without an argument leaves the dot
// untracked.
func walkDefinition(n *parse.TemplateNode, e env) error {
	called := e.defs.t.Lookup(n.Name)
	if called == nil || called.Tree == nil || called.Root == nil {
		return nil
	}
	arg := pipeScope(n.Pipe, e)
	key := invocation{n.Name, arg}
	if e.defs.visited[key] {
		return nil
	}
	e.defs.visited[key] = true
	root := newFrame(nil)
	root.declare("$", arg)
	return walk(called.Root, env{dot: arg, vars: root, defs: e.defs})
}

// walkPipe checks the arguments of a pipeline. With bind, the variables it declares or assigns
// are bound in the current frame: to the scope of the value when the pipeline is a single field,
// and untracked otherwise.
func walkPipe(p *parse.PipeNode, e env, bind bool) error {
	if p == nil {
		return nil
	}
	for _, cmd := range p.Cmds {
		for _, arg := range cmd.Args {
			if err := walk(arg, e); err != nil {
				return err
			}
		}
	}
	if bind && len(p.Decl) > 0 {
		bound := scopeOpaque
		if len(p.Decl) == 1 {
			bound = pipeScope(p, e)
		}
		for _, v := range p.Decl {
			bindVariable(e.vars, p, v.Ident[0], bound)
		}
	}
	return nil
}

func bindVariable(f *frame, p *parse.PipeNode, name string, s scope) {
	if p.IsAssign {
		f.assign(name, s)
		return
	}
	f.declare(name, s)
}

// walkBranch checks an if, with or range. The variables its pipeline declares live in the body
// and the else list: in the body a range binds the element variable to inner and any other
// control binds its variable to value; in the else list they are not tracked.
func walkBranch(b *parse.BranchNode, e env, inner, value scope, isRange bool) error {
	if err := walkPipe(b.Pipe, e, false); err != nil {
		return err
	}
	body := e.inner(inner)
	if b.Pipe != nil {
		for i, v := range b.Pipe.Decl {
			bound := value
			if isRange {
				bound = scopeOpaque
				if i == len(b.Pipe.Decl)-1 {
					bound = inner
				}
			}
			bindVariable(body.vars, b.Pipe, v.Ident[0], bound)
		}
	}
	if err := walk(b.List, body); err != nil {
		return err
	}
	if b.ElseList != nil {
		other := e.inner(e.dot)
		if b.Pipe != nil {
			for _, v := range b.Pipe.Decl {
				bindVariable(other.vars, b.Pipe, v.Ident[0], scopeOpaque)
			}
		}
		return walk(b.ElseList, other)
	}
	return nil
}

// pipeScope is the scope of the value a pipeline yields when it is a single field, variable,
// dot or parenthesised pipeline of those, with chained fields; anything else is not tracked.
func pipeScope(pipe *parse.PipeNode, e env) scope {
	if pipe == nil || len(pipe.Cmds) != 1 || len(pipe.Cmds[0].Args) != 1 {
		return scopeOpaque
	}
	return nodeScope(pipe.Cmds[0].Args[0], e)
}

func nodeScope(node parse.Node, e env) scope {
	switch n := node.(type) {
	case *parse.DotNode:
		return e.dot
	case *parse.FieldNode:
		return fieldScope(e.dot, n.Ident)
	case *parse.VariableNode:
		return fieldScope(e.vars.lookup(n.Ident[0]), n.Ident[1:])
	case *parse.PipeNode:
		return pipeScope(n, e)
	case *parse.ChainNode:
		switch n.Node.(type) {
		case *parse.DotNode, *parse.PipeNode:
			return fieldScope(nodeScope(n.Node, e), n.Field)
		}
	}
	return scopeOpaque
}

// fieldScope is the scope of the value at the path ident below a value of scope from.
func fieldScope(from scope, ident []string) scope {
	if from == scopeOpaque {
		return scopeOpaque
	}
	current := from
	for _, name := range ident {
		current = next(current, name)
	}
	return current
}

// elementScope is the dot inside a range over a value of scope s: the elements of the
// Dependencies list have the dependency's fields, and a string has none that are tracked.
func elementScope(s scope) scope {
	switch s {
	case scopeList:
		return scopeDependency
	case scopeValue:
		return scopeOpaque
	}
	return s
}

func checkField(ident []string, dot scope) error {
	if dot == scopeOpaque || len(ident) == 0 {
		return nil
	}
	current := dot
	for i, name := range ident {
		known, ok := fields[current]
		if !ok {
			// A field of a plain value (a string, a number): nothing below it exists.
			return unknown(ident, i+1)
		}
		found := false
		for _, f := range known {
			if f == name {
				found = true
				break
			}
		}
		if !found {
			return unknown(ident, i+1)
		}
		current = next(current, name)
	}
	return nil
}

// next is the scope of the field name of a value of scope from.
func next(from scope, name string) scope {
	switch {
	case from == scopeRoot && name == "PullRequest":
		return scopePullRequest
	case from == scopeRoot && name == "Supersede":
		return scopeSupersede
	case from == scopeRoot && name == "Dependencies":
		return scopeList
	case from == scopeSupersede && name == "NewPullRequest":
		return scopePullRequest
	}
	return scopeValue
}

// unknown is the error for the variable written as ident, whose first bad part is ident[bad-1].
func unknown(ident []string, bad int) error {
	path := strings.Join(ident, ".")
	if hint, ok := replacements[strings.Join(ident[:bad], ".")]; ok {
		return fmt.Errorf("the variable .%s no longer exists; use %s (docs/specs.html §5.4)", path, hint)
	}
	return fmt.Errorf("the variable .%s does not exist (docs/specs.html §5.4 lists the variables)", path)
}
