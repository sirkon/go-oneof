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
	oo.Line(`// $0 is an interface to limit available implementations to partially replicate discriminated union type functionality`, name)
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
				if subs := oneOfReference(name, v.Type); subs != nil {
					v.Type = subs
					return false
				}
				ast.Inspect(v.Type, func(node ast.Node) bool {
					switch v := node.(type) {
					case *ast.FuncType:
						var madeReplacement bool
						for i, p := range v.Params.List {
							if subs := oneOfReference(name, p.Type); subs != nil {
								v.Params.List[i].Type = subs
								madeReplacement = true
							}
						}
						if v.Results == nil {
							return !madeReplacement
						}
						for i, r := range v.Results.List {
							if subs := oneOfReference(name, r.Type); subs != nil {
								v.Results.List[i].Type = subs
								madeReplacement = true
							}
						}
						return !madeReplacement
					case *ast.StarExpr:
						if subs := oneOfReference(name, v.X); subs != nil {
							v.X = subs
							return false
						}
					case *ast.MapType:
						var madeReplacement bool
						if subs := oneOfReference(name, v.Key); subs != nil {
							v.Key = subs
							madeReplacement = true
						}
						if subs := oneOfReference(name, v.Value); subs != nil {
							v.Value = subs
							madeReplacement = true
						}
						return !madeReplacement
					case *ast.ArrayType:
						if subs := oneOfReference(name, v.Elt); subs != nil {
							v.Elt = subs
							return false
						}
					}
					return true
				})
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

// oneOfReference checks if this expression is oneof reference and returns its substitution. Returns nil otherwise
func oneOfReference(name string, e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.StarExpr:
		switch vv := v.X.(type) {
		case *ast.Ident:
			if vv.Name == oneofPrefix+name {
				obj := vv.Obj
				obj.Name = name
				return &ast.Ident{
					NamePos: vv.NamePos,
					Name:    name,
					Obj:     obj,
				}
			}
		}
	}
	return nil
}
