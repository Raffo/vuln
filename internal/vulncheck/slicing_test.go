// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vulncheck

import (
	"context"
	"path"
	"reflect"
	"testing"

	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages/packagestest"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// funcNames returns a set of function names for `funcs`.
func funcNames(funcs map[*ssa.Function]bool) map[string]bool {
	fs := make(map[string]bool)
	for f := range funcs {
		fs[dbFuncName(f)] = true
	}
	return fs
}

func TestSlicing(t *testing.T) {
	// test program
	p := `
package slice

func X() {}
func Y() {}

// not reachable
func id(i int) int {
        return i
}

// not reachable
func inc(i int) int {
        return i + 1
}

func Apply(b bool, h func()) {
        if b {
                func() {
                        print("applied")
                }()
                return
        }
        h()
}

type I interface {
        Foo()
}

type A struct{}

func (a A) Foo() {}

// not reachable
func (a A) Bar() {}

type B struct{}

func (b B) Foo() {}

func debug(s string) {
        print(s)
}

func Do(i I, input string) {
        debug(input)

        i.Foo()

        func(x string) {
                func(l int) {
                        print(l)
                }(len(x))
        }(input)
}`

	e := packagestest.Export(t, packagestest.Modules, []packagestest.Module{
		{
			Name:  "some/module",
			Files: map[string]interface{}{"slice/slice.go": p},
		},
	})

	graph := NewPackageGraph("go1.18")
	err := graph.LoadPackagesAndMods(e.Config, nil, []string{path.Join(e.Temp(), "/module/slice")}, true)
	if err != nil {
		t.Fatal(err)
	}
	prog, ssaPkgs := ssautil.AllPackages(graph.TopPkgs(), 0)
	prog.Build()

	pkg := ssaPkgs[0]
	sources := map[*ssa.Function]bool{pkg.Func("Apply"): true, pkg.Func("Do"): true}
	fs := funcNames(forwardSlice(sources, cha.CallGraph(prog)))
	want := map[string]bool{
		"Apply":   true,
		"Apply$1": true,
		"X":       true,
		"Y":       true,
		"Do":      true,
		"Do$1":    true,
		"Do$1$1":  true,
		"debug":   true,
		"A.Foo":   true,
		"B.Foo":   true,
	}
	if !reflect.DeepEqual(want, fs) {
		t.Errorf("want %v; got %v", want, fs)
	}
}

func TestCallGraphRemovesGlobalTypeFlow(t *testing.T) {
	const src = `package p

var g, h func()

func Entry() {
	g()
	h()
}

func unrelated() { g = f1 }
func f1()        { h = f2 }
func f2()        {}
`
	e := packagestest.Export(t, packagestest.Modules, []packagestest.Module{{
		Name:  "some/module",
		Files: map[string]interface{}{"p/p.go": src},
	}})
	defer e.Cleanup()

	graph := NewPackageGraph("go1.18")
	if err := graph.LoadPackagesAndMods(e.Config, nil, []string{path.Join(e.Temp(), "module/p")}, true); err != nil {
		t.Fatal(err)
	}
	prog, ssaPkgs := buildSSA(graph.TopPkgs(), graph.TopPkgs()[0].Fset)
	entry := ssaPkgs[0].Func("Entry")
	cg, err := callGraph(context.Background(), prog, []*ssa.Function{entry})
	if err != nil {
		t.Fatal(err)
	}

	got := funcNames(forwardSlice(map[*ssa.Function]bool{entry: true}, cg))
	want := map[string]bool{"Entry": true}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("entry-reachable functions: want %v; got %v", want, got)
	}
}
