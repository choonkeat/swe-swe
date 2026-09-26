package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGitStartsOnlyInRunGit: every git command in swe-swe-server goes through
// runGit (git_run.go), which runs them one at a time per project
// (tasks/2026-09-25-git-one-at-a-time.md). Starting git any other way fails
// here.
func TestGitStartsOnlyInRunGit(t *testing.T) {
	for _, f := range findGitStarts(t, ".") {
		if f.file != "git_run.go" {
			t.Errorf("%s: starts git directly; use runGit, gitRead or gitWrite (git_run.go)", f)
		}
	}
}

// The checker itself: each way of starting git is caught.
func TestFindGitStartsCatches(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "os/exec"

const gitBin = "git"
const other = "ls"

func a() {
	exec.Command("git", "status")
	exec.CommandContext(nil, "git", "fetch")
	exec.Command(gitBin, "log")
	exec.Command("/usr/bin/git", "log")
	exec.Command("sh", "-c", "cd x && git pull")
	exec.Command("bash", "-c", "git status")
	exec.Command("ls", "-la")
	exec.Command(other)
	exec.Command("sh", "-c", "echo gitty")
}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, f := range findGitStarts(t, dir) {
		lines = append(lines, f.line)
	}
	if fmt.Sprint(lines) != "[9 10 11 12 13 14]" {
		t.Errorf("caught lines %v, want [9 10 11 12 13 14]", lines)
	}
}

type gitStart struct {
	file string
	line int
}

func (g gitStart) String() string { return g.file + ":" + strconv.Itoa(g.line) }

// findGitStarts parses every non-test .go file directly in dir and returns
// each exec.Command / exec.CommandContext whose program is git: the literal
// "git", a path ending in /git, a constant holding either, or sh/bash -c with
// a script that runs git.
func findGitStarts(t *testing.T, dir string) []gitStart {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}

	// String constants and vars of the package, so exec.Command(gitBin) counts.
	consts := map[string]string{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if s, ok := stringLit(vs.Values[i]); ok {
						consts[name.Name] = s
					}
				}
			}
			return true
		})
	}
	str := func(e ast.Expr) (string, bool) {
		if s, ok := stringLit(e); ok {
			return s, true
		}
		if id, ok := e.(*ast.Ident); ok {
			s, ok := consts[id.Name]
			return s, ok
		}
		return "", false
	}
	isGit := func(s string) bool { return s == "git" || strings.HasSuffix(s, "/git") }

	var out []gitStart
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "exec" {
				return true
			}
			args := call.Args
			switch sel.Sel.Name {
			case "Command":
			case "CommandContext":
				if len(args) > 0 {
					args = args[1:]
				}
			default:
				return true
			}
			if len(args) == 0 {
				return true
			}
			prog, _ := str(args[0])
			hit := isGit(prog)
			if prog == "sh" || prog == "bash" {
				for _, a := range args[1:] {
					if s, ok := str(a); ok && (strings.HasPrefix(s, "git ") || strings.Contains(s, " git ")) {
						hit = true
					}
				}
			}
			if hit {
				pos := fset.Position(call.Pos())
				out = append(out, gitStart{file: filepath.Base(pos.Filename), line: pos.Line})
			}
			return true
		})
	}
	return out
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
