package main

import (
	"go/ast"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestDiagnosticFieldsOnlyReadInRenderDiagnostic is the AD-8 enforcement test
// committed to during epic planning: every Diagnostic, regardless of whether
// core/engine or cmd/cli produced it, must be printed through
// renderDiagnostic. This uses type information (not just identifier names)
// to verify nothing else in either package reads an engine.Diagnostic's
// Severity, Message, or Reason field — i.e. nobody can bypass the shared
// renderer with a stray fmt.Println.
func TestDiagnosticFieldsOnlyReadInRenderDiagnostic(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(cfg, "nats/cmd/cli", "nats/core/engine")
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	fieldNames := map[string]bool{"Severity": true, "Message": true, "Reason": true}

	var readingFuncs []string
	total := 0

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			t.Fatalf("package %s failed to load: %v", pkg.PkgPath, pkg.Errors)
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				funcName := fn.Name.Name

				ast.Inspect(fn, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok || !fieldNames[sel.Sel.Name] {
						return true
					}
					selection, ok := pkg.TypesInfo.Selections[sel]
					if !ok || selection.Recv() == nil {
						return true
					}
					if !strings.Contains(selection.Recv().String(), "core/engine.Diagnostic") {
						return true
					}
					total++
					if !(pkg.PkgPath == "nats/cmd/cli" && funcName == "renderDiagnostic") {
						readingFuncs = append(readingFuncs, pkg.PkgPath+"."+funcName)
					}
					return true
				})
			}
		}
	}

	if total == 0 {
		t.Fatal("expected at least one Diagnostic field read across cmd/cli and core/engine — test setup is broken")
	}
	if len(readingFuncs) != 0 {
		t.Fatalf("Diagnostic fields must only be read inside cmd/cli's renderDiagnostic (AD-8); found reads in: %v", readingFuncs)
	}
}
