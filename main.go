package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/sirkon/gosrcfmt"
	"github.com/sirkon/gotify"
	"github.com/sirkon/message"
)

const (
	oneofPrefix = "oneof"
)

func main() {
	var args struct {
		Pointer bool   `arg:"-p" help:"implement oneof interface over pointer to struct"`
		FILE    string `arg:"positional,required" help:"file path to process"`
	}
	p := arg.MustParse(&args)

	if !strings.HasSuffix(args.FILE, ".go") {
		p.Fail("FILE must be go file")
	}

	// parse input file
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, args.FILE, nil, parser.AllErrors|parser.ParseComments)
	if err != nil {
		lst, ok := err.(scanner.ErrorList)
		if !ok {
			message.Fatal(err)
		}
		for _, l := range lst {
			message.Error(l)
		}
		os.Exit(1)
	}

	// prepare name gotifier
	gotifier := gotify.New(nil)

	// look for `oneofXXX` strucute. It must be only one in the file at the top level
	var def *ast.StructType
	var name string
	var origOneof bytes.Buffer
	ast.Inspect(file, func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.File:
			return true
		case *ast.TypeSpec:
			var ok bool
			var newDef *ast.StructType
			newDef, ok = v.Type.(*ast.StructType)
			if !ok {
				return true
			}
			if !strings.HasPrefix(v.Name.Name, oneofPrefix) {
				return true
			}
			if def != nil {
				message.Fatalf("%s: duplicate oneof in this file, the previous one was %s%s", fset.Position(v.Pos()), oneofPrefix, name)
			}
			def = newDef
			name = v.Name.Name[len(oneofPrefix):]
			if name != gotifier.Public(name) {
				message.Fatalf("%s: name must be %s%s, got %s", fset.Position(v.Name.NamePos), oneofPrefix, gotifier.Public(name), v.Name.Name)
			}
			var errCount int
			for _, f := range def.Fields.List {
				if len(f.Names) == 0 {
					errCount++
					message.Errorf("%s: embedding is not allowed for oneofs", fset.Position(f.Pos()))
					continue
				}
				branchName := f.Names[0].Name
				if branchName != gotifier.Public(branchName) {
					errCount++
					message.Errorf("%s: invalid branch name %s for oneof branch, must be %s", fset.Position(f.Names[0].NamePos), branchName, gotifier.Public(branchName))
				}
			}
			if errCount > 0 {
				message.Fatalf("cannot continue")
			}
			origOneof.WriteString(oneofPrefix)
			origOneof.WriteString(name)
			origOneof.WriteByte(' ')
			if err := printer.Fprint(&origOneof, fset, newDef); err != nil {
				message.Errorf("render original %s%s structure", oneofPrefix, name)
			}
			return true
		default:
			return true
		}
	})

	if def == nil {
		message.Fatalf("%s:1 no oneof candidates found", args.FILE)
	}

	// render oneof code
	var oo Collector

	// render oneof interface
	oo.Line(`// $0 an interface to limit available implementations to emulate discriminated union type`, name)
	oo.Line(`type $0 interface {`, name)
	oo.Line(`    is$0()`, name)
	oo.Line(`}`)
	oo.Newl()

	// render implementations
	for _, f := range def.Fields.List {
		var buf bytes.Buffer
		branchName := f.Names[0].Name
		buf.WriteString(branchName)
		buf.WriteByte(' ')
		// now dive to a substruct to change
		ast.Inspect(f.Type, func(node ast.Node) bool {
			switch v := node.(type) {
			case *ast.Field:
				vv, ok := v.Type.(*ast.StarExpr)
				if !ok {
					return true
				}
				if vvv, ok := vv.X.(*ast.Ident); ok {
					if vvv.Name == oneofPrefix+name {
						obj := vvv.Obj
						obj.Name = name
						vv.X = &ast.Ident{
							NamePos: vvv.NamePos,
							Name:    name,
							Obj:     obj,
						}
					}
					v.Type = vv.X
				}
			}
			return true
		})
		if err := printer.Fprint(&buf, fset, f.Type); err != nil {
			message.Fatalf("rendering branch for field %s: %s", branchName, err)
		}
		oo.Line(`// $0 branch of $1`, branchName, name)
		oo.Line(`type $0`, buf.String())
		var possiblePtr string
		if _, ok := f.Type.(*ast.StructType); ok {
			possiblePtr = "*"
		}
		oo.Line(`func ($0$1) is$2() {}`, possiblePtr, branchName, name)
		oo.Newl()
	}

	// render new file. Borrow package name, import statements and oneof definition from the source file
	var dest Collector
	noImportsYet := true
	ast.Inspect(file, func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.File:
			dest.Line(`package $0`, v.Name.Name)
			dest.Newl()
			_, base := filepath.Split(args.FILE)
			if args.Pointer {
				dest.Line(`//go:generate go-oneof --pointer $0`, base)
			} else {
				dest.Line(`//go:generate go-oneof $0`, base)
			}
			dest.Newl()

			return true
		case *ast.ImportSpec:
			if noImportsYet {
				noImportsYet = false
				dest.Rawl(`import (`)
			}
			if v.Name != nil {
				dest.Line(`$0 $1`, v.Name.Name, v.Path.Value)
			} else {
				dest.Line(`$0`, v.Path.Value)
			}
		case *ast.TypeSpec:
			if !noImportsYet {
				dest.Rawl(`)`)
				dest.Newl()
				noImportsYet = true
			}
			if vv, ok := v.Type.(*ast.StructType); ok && vv == def {
				var buf bytes.Buffer
				buf.WriteString("type ")
				buf.Write(origOneof.Bytes())
				dest.Rawl(buf.String())
				dest.Newl()
			}
			return false
		}
		return true
	})

	dest.Rawl(oo.String())
	fset = token.NewFileSet()
	file, err = parser.ParseFile(fset, "", dest.Bytes(), parser.ParseComments|parser.AllErrors)
	res, err := gosrcfmt.Source(dest.Bytes(), "<output>")
	if err != nil {
		var buf bytes.Buffer
		buf.WriteString(err.Error())
		buf.WriteByte('\n')
		lines := strings.Split(dest.String(), "\n")
		errFmt := fmt.Sprintf("%%0%dd", len(strconv.Itoa(len(lines)+1)))
		for i, l := range lines {
			_, _ = fmt.Fprintf(&buf, errFmt, i+1)
			buf.WriteByte(' ')
			buf.WriteString(l)
			buf.WriteByte('\n')
		}
		message.Fatal(buf.String())
	}

	if err := ioutil.WriteFile(args.FILE, res, 0644); err != nil {
		message.Fatal(err)
	}
}
