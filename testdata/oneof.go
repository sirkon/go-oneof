package node

//go:generate go-oneof --pointer oneof.go

type oneofNode struct {
	Value       string
	OperatorSum struct {
		Left      *oneofNode
		Right     *oneofNode
		Appendix  map[string]*oneofNode
		Payload   []*oneofNode
		Activator func(node *oneofNode)
	}
}

// Node is an interface to limit available implementations to partially replicate discriminated union type functionality
type Node interface {
	isNode()
}

// Value branch of Node
type Value string

func (Value) isNode() {}

// OperatorSum branch of Node
type OperatorSum struct {
	Left      Node
	Right     Node
	Appendix  map[string]Node
	Payload   []Node
	Activator func(node Node)
}

func (*OperatorSum) isNode() {}
