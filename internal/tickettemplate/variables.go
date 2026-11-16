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

// checkVariables refuses a template that names a field the context does not have. Execution
// would fail on the same fields, but only on the branches it takes; walking the parse tree
// covers every branch when the template is saved.
func checkVariables(t *template.Template) error {
	if t.Tree == nil || t.Root == nil {
		return nil
	}
	return walk(t.Root, scopeRoot)
}

func walk(node parse.Node, dot scope) error {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return nil
		}
		for _, c := range n.Nodes {
			if err := walk(c, dot); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return walk(n.Pipe, dot)
	case *parse.PipeNode:
		if n == nil {
			return nil
		}
		for _, cmd := range n.Cmds {
			for _, arg := range cmd.Args {
				if err := walk(arg, dot); err != nil {
					return err
				}
			}
		}
	case *parse.FieldNode:
		return checkField(n.Ident, dot)
	case *parse.VariableNode:
		if n.Ident[0] == "$" {
			return checkField(n.Ident[1:], scopeRoot)
		}
	case *parse.ChainNode:
		switch base := n.Node.(type) {
		case *parse.DotNode:
			return checkField(n.Field, dot)
		case *parse.PipeNode:
			if err := walk(base, dot); err != nil {
				return err
			}
			return checkField(n.Field, pipeScope(base, dot))
		default:
			return walk(n.Node, dot)
		}
	case *parse.IfNode:
		return walkBranch(&n.BranchNode, dot, dot)
	case *parse.WithNode:
		return walkBranch(&n.BranchNode, dot, pipeScope(n.Pipe, dot))
	case *parse.RangeNode:
		return walkBranch(&n.BranchNode, dot, elementScope(pipeScope(n.Pipe, dot)))
	case *parse.TemplateNode:
		return walk(n.Pipe, dot)
	}
	return nil
}

func walkBranch(b *parse.BranchNode, dot, inner scope) error {
	if err := walk(b.Pipe, dot); err != nil {
		return err
	}
	if err := walk(b.List, inner); err != nil {
		return err
	}
	if b.ElseList != nil {
		return walk(b.ElseList, dot)
	}
	return nil
}

// pipeScope is the scope of the value a pipeline yields when it is a single field or the dot
// itself, which is the dot inside a with over it; anything else is not tracked.
func pipeScope(pipe *parse.PipeNode, dot scope) scope {
	if pipe == nil || len(pipe.Cmds) != 1 || len(pipe.Cmds[0].Args) != 1 || dot == scopeOpaque {
		return scopeOpaque
	}
	switch arg := pipe.Cmds[0].Args[0].(type) {
	case *parse.DotNode:
		return dot
	case *parse.FieldNode:
		current := dot
		for _, name := range arg.Ident {
			current = next(current, name)
		}
		return current
	}
	return scopeOpaque
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
