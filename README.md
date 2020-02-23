# go-oneof

This tool is aimed to generate a Go code to partially replicate lacking discriminated unions functionality in Go

Installation

```shell script
GO111MODULE=on go get github.com/sirkon/go-oneof
```

## How to use it

Need to write a special structure with name staring from `oneof` (there are further limitations, but they will be 
reported by the utility itself). Like this:

```go
// node.go file

package node

//go:generate go-oneof node.go

type oneofNode struct {
    Value string
    OperatorSum struct {
        Left  Node
        Right Node
    }
}
```

And run `go generate` over it

The result will be

```go
package node

//go:generate go-oneof node.go

type oneofNode struct {
	Value       string
	OperatorSum struct {
		Left  Node
		Right Node
	}
}

// Node an interface to limit available implementations to emulate discriminated union type
type Node interface {
	isNode()
}

// Value branch of Node
type Value string

func (Value) isNode() {}

// OperatorSum branch of Node
type OperatorSum struct {
	Left  Node
	Right Node
}

func (OperatorSum) isNode() {}
```

Some notes:

* type `oneofName` will generate interface `Name` with private method `isName`
* each field causes an implementation of generated `Name`
* comments existed in the source file will disappear in its replacement
* free comments will stay, but will be moved on the top of the file
* You may use `--pointer` or `-p` call parameter to implement `Name` of source struct fields over pointer to these 
 structs, not structs themselves, i.e. `Node` implementation for `OperatorSum` would be  
 
     ```go
     func (*OperatorSum) isNode()
     ```
  in this example
* This source file may have errors like reference to entities that do not exists. This allow a self reference before
the first generation.