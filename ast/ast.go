package ast

import (
	"bolt/token"
	"bytes"
	"strings"
)

type Node interface {
	TokenLiteral() string
	String() string
	Line() int
}

type Statement interface {
	Node
	statementNode()
}

type Expression interface {
	Node
	expressionNode()
}

type Program struct {
	Statements []Statement
}

func (p *Program) Line() int { return -1 }

func (p *Program) String() string {
	var out bytes.Buffer

	for _, s := range p.Statements {
		out.WriteString(s.String())
	}

	return out.String()
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	} else {
		return ""
	}
}

type BreakStatement struct {
	LineNum int
	Token   token.Token
}

func (ls *BreakStatement) String() string {
	return "break"
}

func (ls *BreakStatement) statementNode()       {}
func (ls *BreakStatement) Line() int            { return ls.LineNum }
func (ls *BreakStatement) TokenLiteral() string { return ls.Token.Literal }

type LetStatement struct {
	LineNum int
	Token   token.Token
	Name    *Identifier
	Value   Expression
}

func (ls *LetStatement) String() string {
	var out bytes.Buffer

	out.WriteString(ls.TokenLiteral() + " ")
	out.WriteString(ls.Name.String())
	out.WriteString(" = ")

	if ls.Value != nil {
		out.WriteString(ls.Value.String())
	}

	out.WriteString(";")
	return out.String()
}

func (ls *LetStatement) statementNode()       {}
func (ls *LetStatement) Line() int            { return ls.LineNum }
func (ls *LetStatement) TokenLiteral() string { return ls.Token.Literal }

type Identifier struct {
	LineNum int
	Token   token.Token
	Value   string
}

func (i *Identifier) String() string {
	return i.Value
}
func (i *Identifier) expressionNode()      {}
func (i *Identifier) Line() int            { return i.LineNum }
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }

type ReturnStatement struct {
	LineNum     int
	Token       token.Token
	ReturnValue Expression
}

func (rs *ReturnStatement) String() string {
	var out bytes.Buffer

	out.WriteString(rs.TokenLiteral() + " ")
	if rs.ReturnValue != nil {
		out.WriteString(rs.ReturnValue.String())
	}

	out.WriteString(";")
	return out.String()
}
func (rs *ReturnStatement) statementNode()       {}
func (rs *ReturnStatement) Line() int            { return rs.LineNum }
func (rs *ReturnStatement) TokenLiteral() string { return rs.Token.Literal }

type ExpressionStatement struct {
	LineNum    int
	Token      token.Token
	Expression Expression
}

func (es *ExpressionStatement) String() string {
	if es.Expression != nil {
		return es.Expression.String()
	}
	return ""
}
func (es *ExpressionStatement) statementNode()       {}
func (es *ExpressionStatement) Line() int            { return es.LineNum }
func (es *ExpressionStatement) TokenLiteral() string { return es.Token.Literal }

type IntegerLiteral struct {
	LineNum int
	Token   token.Token
	Value   int64
}

func (il *IntegerLiteral) String() string {
	return il.Token.Literal
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) Line() int            { return il.LineNum }
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }

type FloatLiteral struct {
	LineNum int
	Token   token.Token
	Value   float64
}

func (fl *FloatLiteral) String() string       { return fl.Token.Literal }
func (fl *FloatLiteral) expressionNode()      {}
func (fl *FloatLiteral) Line() int            { return fl.LineNum }
func (fl *FloatLiteral) TokenLiteral() string { return fl.Token.Literal }

type PrefixExpression struct {
	LineNum  int
	Token    token.Token
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode() {}
func (pe *PrefixExpression) Line() int       { return pe.LineNum }

func (pe *PrefixExpression) TokenLiteral() string { return pe.Token.Literal }
func (pe *PrefixExpression) String() string {
	var out bytes.Buffer

	out.WriteString("(" + pe.Operator + pe.Right.String() + ")")

	return out.String()
}

type InfixExpression struct {
	LineNum  int
	Token    token.Token
	Operator string
	Left     Expression
	Right    Expression
}

func (ie *InfixExpression) expressionNode()      {}
func (ie *InfixExpression) Line() int            { return ie.LineNum }
func (ie *InfixExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *InfixExpression) String() string {
	var out bytes.Buffer

	out.WriteString("(" + ie.Left.String() + " " + ie.Operator + " " + ie.Right.String() + ")")
	return out.String()
}

type Boolean struct {
	LineNum int
	Token   token.Token
	Value   bool
}

func (b *Boolean) expressionNode()      {}
func (b *Boolean) Line() int            { return b.LineNum }
func (b *Boolean) TokenLiteral() string { return b.Token.Literal }
func (b *Boolean) String() string {
	return b.TokenLiteral()
}

type IfExpression struct {
	LineNum     int
	Token       token.Token
	Condition   Expression
	Consequence *BlockStatement
	Alternative *IfExpression
}

func (ie *IfExpression) expressionNode() {}
func (ie *IfExpression) Line() int       { return ie.LineNum }

func (ie *IfExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IfExpression) String() string {
	var out bytes.Buffer
	// TODO: handle else if
	out.WriteString("if")
	if ie.Condition != nil {
		out.WriteString(ie.Condition.String())
	}
	out.WriteString(" ")
	out.WriteString(ie.Consequence.String())
	if ie.Alternative != nil {
		out.WriteString("else ")
		out.WriteString(ie.Alternative.String())
	}
	return out.String()
}

type BlockStatement struct {
	LineNum    int
	Token      token.Token
	Statements []Statement
}

func (bs *BlockStatement) statementNode()       {}
func (bs *BlockStatement) Line() int            { return bs.LineNum }
func (bs *BlockStatement) TokenLiteral() string { return bs.Token.Literal }
func (bs *BlockStatement) String() string {
	var out bytes.Buffer

	for _, s := range bs.Statements {
		out.WriteString(s.String())
	}

	return out.String()
}

type FunctionLiteral struct {
	LineNum    int
	Token      token.Token
	Parameters []*Identifier
	Body       *BlockStatement
}

func (fl *FunctionLiteral) expressionNode()      {}
func (fl *FunctionLiteral) Line() int            { return fl.LineNum }
func (fl *FunctionLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FunctionLiteral) String() string {
	var out bytes.Buffer
	params := []string{}
	for _, p := range fl.Parameters {
		params = append(params, p.String())
	}

	out.WriteString(fl.TokenLiteral())
	out.WriteString("(")
	out.WriteString(strings.Join(params, ","))
	out.WriteString(")")
	out.WriteString("{ " + fl.Body.String() + " }")

	return out.String()
}

type CallExpression struct {
	LineNum   int
	Token     token.Token
	Function  Expression // Identifier or FunctionLiteral
	Arguments []Expression
}

func (ce *CallExpression) expressionNode() {}
func (ce *CallExpression) Line() int       { return ce.LineNum }

func (ce *CallExpression) TokenLiteral() string { return ce.Token.Literal }

func (ce *CallExpression) String() string {
	var out bytes.Buffer
	args := []string{}

	for _, a := range ce.Arguments {
		args = append(args, a.String())
	}

	out.WriteString(ce.Function.String())
	out.WriteString("(")
	out.WriteString(strings.Join(args, ", "))
	out.WriteString(")")
	return out.String()
}

type StringLiteral struct {
	LineNum int
	Token   token.Token
	Value   string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) Line() int            { return sl.LineNum }
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }
func (sl *StringLiteral) String() string       { return sl.TokenLiteral() }

type ArrayLiteral struct {
	LineNum  int
	Token    token.Token
	Elements []Expression
}

func (al *ArrayLiteral) expressionNode()      {}
func (al *ArrayLiteral) Line() int            { return al.LineNum }
func (al *ArrayLiteral) TokenLiteral() string { return al.Token.Literal }
func (al *ArrayLiteral) String() string {
	var out bytes.Buffer
	elements := []string{}

	for _, el := range al.Elements {
		elements = append(elements, el.String())
	}

	out.WriteString("[")
	out.WriteString(strings.Join(elements, ", "))
	out.WriteString("]")

	return out.String()
}

type IndexExpression struct {
	LineNum int
	Token   token.Token
	Left    Expression
	Index   Expression
}

func (ie *IndexExpression) expressionNode()      {}
func (ie *IndexExpression) Line() int            { return ie.LineNum }
func (ie *IndexExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IndexExpression) String() string {
	return "(" + ie.Left.String() + "[" + ie.Index.String() + "])"
}

type SliceExpression struct {
	LineNum    int
	Token      token.Token
	Left       Expression
	IndexStart Expression
	IndexEnd   Expression
}

func (se *SliceExpression) expressionNode()      {}
func (se *SliceExpression) Line() int            { return se.LineNum }
func (se *SliceExpression) TokenLiteral() string { return se.Token.Literal }
func (se *SliceExpression) String() string {
	return "(" + se.Left.String() + "[" + se.IndexStart.String() + ":" + se.IndexEnd.String() + "])"
}

type HashLiteral struct {
	LineNum int
	Token   token.Token
	Pairs   map[Expression]Expression
}

func (hl *HashLiteral) expressionNode()      {}
func (hl *HashLiteral) Line() int            { return hl.LineNum }
func (hl *HashLiteral) TokenLiteral() string { return hl.Token.Literal }
func (hl *HashLiteral) String() string {
	pairs := []string{}
	for key, val := range hl.Pairs {
		pairs = append(pairs, key.String()+" : "+val.String())
	}

	return "{" + strings.Join(pairs, ", ") + "}"
}

type ForExpression struct {
	LineNum     int
	Token       token.Token
	Condition   Expression
	Consequence *BlockStatement
}

func (fe *ForExpression) expressionNode()      {}
func (fe *ForExpression) Line() int            { return fe.LineNum }
func (fe *ForExpression) TokenLiteral() string { return fe.Token.Literal }
func (fe *ForExpression) String() string {
	return "for " + fe.Condition.String() + "{\n" + fe.Consequence.String() + "\n}"
}

type RangeExpression struct {
	LineNum int
	Token   token.Token
	Val1    *Identifier
	Val2    *Identifier
	Ranging Expression
}

func (re *RangeExpression) expressionNode()      {}
func (re *RangeExpression) Line() int            { return re.LineNum }
func (re *RangeExpression) TokenLiteral() string { return re.Token.Literal }
func (re *RangeExpression) String() string {
	val2str := ""
	if re.Val2 != nil {
		val2str = ", " + re.Val2.String()
	}
	return re.Val1.String() + val2str + " = range " + re.Ranging.String()
}

type StructType struct {
	LineNum int
	Token   token.Token
	Name    *Identifier
	Fields  []*Identifier
}

func (st *StructType) statementNode()       {}
func (st *StructType) Line() int            { return st.LineNum }
func (st *StructType) TokenLiteral() string { return st.Token.Literal }
func (st *StructType) String() string {
	fields := []string{}
	for _, f := range st.Fields {
		fields = append(fields, f.Value)
	}
	return "struct " + st.Name.Value + " { " + strings.Join(fields, ", ") + "}"
}

type StructInstantiation struct {
	LineNum int
	Token   token.Token
	Left    Expression
	Fields  map[string]Expression
}

func (si *StructInstantiation) expressionNode()      {}
func (si *StructInstantiation) Line() int            { return si.LineNum }
func (si *StructInstantiation) TokenLiteral() string { return si.Token.Literal }
func (si *StructInstantiation) String() string {
	fields := []string{}
	for id, val := range si.Fields {
		fields = append(fields, id+" : "+val.String())
	}
	return si.Left.String() + " { " + strings.Join(fields, ", ") + "}"
}

type FieldAccess struct {
	LineNum int
	Token   token.Token
	Left    Expression
	Right   Expression
}

func (fa *FieldAccess) expressionNode()      {}
func (fa *FieldAccess) Line() int            { return fa.LineNum }
func (fa *FieldAccess) TokenLiteral() string { return fa.Token.Literal }
func (fa *FieldAccess) String() string       { return fa.Left.String() + "." + fa.Right.String() }

type ImportStatement struct {
	LineNum int
	Token   token.Token
	Value   string
}

func (is *ImportStatement) statementNode()       {}
func (is *ImportStatement) Line() int            { return is.LineNum }
func (is *ImportStatement) TokenLiteral() string { return is.Token.Literal }
func (is *ImportStatement) String() string       { return "import " + is.Value }

type PubStatement struct {
	LineNum int
	Token   token.Token
	Stmt    Statement
}

func (p *PubStatement) statementNode()       {}
func (p *PubStatement) Line() int            { return p.LineNum }
func (p *PubStatement) TokenLiteral() string { return p.Token.Literal }
func (p *PubStatement) String() string       { return "pub " + p.Stmt.String() }
