/* package node sample */

package node

//go:generate go-oneof --pointer oneof.go

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

func (*OperatorSum) isNode() {}
