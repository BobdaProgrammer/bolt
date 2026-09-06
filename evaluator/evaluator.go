package evaluator

import (
	"bolt/ast"
	"bolt/object"
	"fmt"
	"strings"
)

var (
	NULL  = &object.Null{}
	TRUE  = &object.Boolean{Value: true}
	FALSE = &object.Boolean{Value: false}
)

func newError(format string, a ...interface{}) *object.Error {
	return &object.Error{Message: fmt.Sprintf(format, a...)}
}

func isError(obj object.Object) bool {
	if obj != nil {
		return obj.Type() == object.ERROR_OBJ
	}
	return false
}

func Eval(node ast.Node, env *object.Environment) object.Object {
	switch node := node.(type) {
	case *ast.Program:
		return evalProgram(node.Statements, env)
	case *ast.ExpressionStatement:
		return Eval(node.Expression, env)
	case *ast.IntegerLiteral:
		return &object.Integer{Value: node.Value}
	case *ast.FloatLiteral:
		return &object.Float{Value: node.Value}
	case *ast.HashLiteral:
		return evalHashLiteral(node, env)
	case *ast.Boolean:
		return nativeBoolToBooleanObject(node.Value)
	case *ast.PrefixExpression:
		right := Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalPrefixExpression(node.Operator, right)
	case *ast.InfixExpression:
		left := Eval(node.Left, env)
		right := Eval(node.Right, env)
		if isError(left) {
			return left
		}
		if isError(right) {
			return right
		}
		return evalInfixExpression(node.Operator, left, right)
	case *ast.BlockStatement:
		return evalBlockStatement(node, env)
	case *ast.IfExpression:
		return evalIfExpression(node, env)
	case *ast.ReturnStatement:
		val := Eval(node.ReturnValue, env)
		if isError(val) {
			return val
		}
		return &object.ReturnValue{Value: val}
	case *ast.LetStatement:
		val := Eval(node.Value, env)
		if isError(val) {
			return val
		}
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env)
	case *ast.FunctionLiteral:
		params := node.Parameters
		body := node.Body
		return &object.Function{Parameters: params, Body: body, Env: env}
	case *ast.CallExpression:
		function := Eval(node.Function, env)
		if isError(function) {
			return function
		}
		args := evalExpressions(node.Arguments, env)
		if len(args) == 1 && isError(args[0]) {
			return args[0]
		}
		return applyFunction(function, args)
	case *ast.StringLiteral:
		return &object.String{Value: node.Value}
	case *ast.ArrayLiteral:
		elements := evalExpressions(node.Elements, env)
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return &object.Array{Elements: elements}
	case *ast.IndexExpression:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		index := Eval(node.Index, env)
		if isError(index) {
			return index
		}
		return evalIndexExpression(left, index)
	case *ast.SliceExpression:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		firstInd := Eval(node.IndexStart, env)
		if isError(firstInd) {
			return firstInd
		}
		secondInd := Eval(node.IndexEnd, env)
		if isError(secondInd) {
			return secondInd
		}
		return evalSliceExpression(left, firstInd, secondInd)
	}
	return nil
}

func evalHashLiteral(node *ast.HashLiteral, env *object.Environment) object.Object {
	pairs := make(map[object.HashKey]object.HashPair)
	for keyNode, valueNode := range node.Pairs {
		key := Eval(keyNode, env)
		if isError(key) {
			return key
		}

		hashKey, ok := key.(object.Hashable)
		if !ok {
			return newError("Unusable as hash key: %s", key.Type())
		}

		value := Eval(valueNode, env)
		if isError(value) {
			return value
		}

		hashed := hashKey.HashKey()
		pairs[hashed] = object.HashPair{Key: key, Value: value}
	}
	return &object.Hash{Pairs: pairs}
}

func evalSliceExpression(left, firstInd, secondInd object.Object) object.Object {
	if left.Type() != object.ARRAY_OBJ {
		return newError("Array slicing operator not supported on: %s", left.Type())
	}

	if firstInd.Type() == object.INTEGER_OBJ && secondInd.Type() == object.INTEGER_OBJ {
		arrObj := left.(*object.Array)
		firstIndObj := firstInd.(*object.Integer)
		secondIndObj := secondInd.(*object.Integer)
		if int(firstIndObj.Value) <= len(arrObj.Elements) && firstIndObj.Value >= 0 && int(secondIndObj.Value) <= len(arrObj.Elements) && secondIndObj.Value >= 0 {
			return &object.Array{Elements: arrObj.Elements[firstIndObj.Value:secondIndObj.Value]}
		} else {
			return newError("Attempting to access index outside of array bounds %s[%d:%d]", left.Type(), firstIndObj.Value, secondIndObj.Value)
		}
	} else {
		return newError("Values in array slicing expression must be integer %s[%s:%s]", left.Type(), firstInd.Type(), secondInd.Type())
	}
}

func evalHashIndexExpression(left, index object.Object) object.Object {
	hashObject := left.(*object.Hash)

	key, ok := index.(object.Hashable)
	if !ok {
		return newError("Unusable as hash key: %s", index.Type())
	}

	pair, ok := hashObject.Pairs[key.HashKey()]
	if !ok {
		return NULL
	}

	return pair.Value
}

func evalIndexExpression(left, index object.Object) object.Object {
	switch {
	case left.Type() == object.ARRAY_OBJ && index.Type() == object.INTEGER_OBJ:
		return evalArrayIndexExpression(left, index)
	case left.Type() == object.HASH_OBJ:
		return evalHashIndexExpression(left, index)
	default:
		return newError("Index operator not supported: %s", left.Type())
	}
}

func evalArrayIndexExpression(array, index object.Object) object.Object {
	arrayObject := array.(*object.Array)
	idx := index.(*object.Integer).Value
	max := int64(len(arrayObject.Elements) - 1)
	if idx < 0 || idx > max {
		return NULL
	}

	return arrayObject.Elements[idx]
}

func applyFunction(fn object.Object, args []object.Object) object.Object {
	switch fn := fn.(type) {
	case *object.Function:
		if len(fn.Parameters) != len(args) {
			return newError("Function requires %d arguments, recieved %d", len(fn.Parameters), len(args))
		}
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *object.Builtin:
		return fn.Fn(args...)
	default:
		return newError("Not a function: %s", fn.Type())
	}
}

func extendFunctionEnv(fn *object.Function, args []object.Object) *object.Environment {
	env := object.NewEnclosedEnvironment(fn.Env)
	for paramIdx, param := range fn.Parameters {
		env.Set(param.Value, args[paramIdx])
	}

	return env
}
func unwrapReturnValue(obj object.Object) object.Object {
	if returnValue, ok := obj.(*object.ReturnValue); ok {
		return returnValue.Value
	}
	return obj
}

func evalExpressions(exps []ast.Expression, env *object.Environment) []object.Object {
	var result []object.Object

	for _, e := range exps {
		evaluated := Eval(e, env)
		if isError(evaluated) {
			return []object.Object{evaluated}
		}
		result = append(result, evaluated)
	}
	return result
}

func evalIdentifier(node *ast.Identifier, env *object.Environment) object.Object {
	if val, ok := env.Get(node.Value); ok {
		return val
	}

	if builtin, ok := builtins[node.Value]; ok {
		return builtin
	}

	return newError("Identifier not found: " + node.Value)
}

// In the evalProgram, nested returns wouldn't work. This is because it only stops for a return if the current statement type is a return value. This is a problem because eva:Program could be on a if expression for example and even if that if statement has a return inside that returns something it wouldn't use that value since it has been given the return value and not the return type. This means that when it checks if the value is a statement, it will be false since the return value isnt a return statement but an expression like an integer or boolean. However, with this implementation, if we are on an if statement with a return inside, when the return is evaluated, the value isn't passed back up, the statement is. This therefore means that it does register that there is a return block since it is given a whole statement.
func evalBlockStatement(block *ast.BlockStatement, env *object.Environment) object.Object {
	var result object.Object

	for _, statement := range block.Statements {
		result = Eval(statement, env)

		if result != nil {
			rt := result.Type()
			if rt == object.RETURN_VALUE_OBJ || rt == object.ERROR_OBJ {
				return result
			}
		}
	}
	return result
}

func evalIfExpression(ie *ast.IfExpression, env *object.Environment) object.Object {
	condition := Eval(ie.Condition, env)
	if isError(condition) {
		return condition
	}
	if isTruthy(condition) {
		return Eval(ie.Consequence, env)
	} else if ie.Alternative != nil {
		return Eval(ie.Alternative, env)
	} else {
		return NULL
	}
}

func isTruthy(obj object.Object) bool {
	switch obj {
	case NULL:
		return false
	case TRUE:
		return true
	case FALSE:
		return false
	default:
		return true
	}
}

func evalInfixExpression(operator string, left, right object.Object) object.Object {
	if left == nil || right == nil {
		if left == nil {
			return newError("Unknown left side of infix expression: nil")
		}
		if right == nil {
			return newError("Unknown right side of infix expression: nil")
		}
	}
	switch {
	case left.Type() == object.INTEGER_OBJ && right.Type() == object.INTEGER_OBJ:
		return evalIntegerInfixExpression(operator, left, right)
	case isNumber(left.Type()) && isNumber(right.Type()):
		l, r := convertInfixNumsToFloat(left, right)
		return evalFloatInfixExpression(operator, l, r)
	case left.Type() == object.BOOLEAN_OBJ && right.Type() == object.BOOLEAN_OBJ:
		return evalBooleanInfixExpression(operator, left, right)
	case left.Type() == object.STRING_OBJ && right.Type() == object.STRING_OBJ:
		return evalStringInfixExpression(operator, left, right)
	case (left.Type() == object.STRING_OBJ && right.Type() == object.INTEGER_OBJ) || (left.Type() == object.INTEGER_OBJ && right.Type() == object.STRING_OBJ):
		return evalStringAndIntegerInfixExpression(operator, left, right)
	case left.Type() != right.Type():
		return newError("Type mismatch on infix expression: %s %s %s", left.Type(), operator, right.Type())
	default:
		return newError("Unknown operator '%s' in: %s %s %s", operator, left.Type(), operator, right.Type())
	}
}

func convertInfixNumsToFloat(left, right object.Object) (object.Object, object.Object) {
	l, r := left, right
	if left.Type() == object.INTEGER_OBJ {
		leftVal := l.(*object.Integer)
		l = &object.Float{Value: float64(leftVal.Value)}
	}
	if right.Type() == object.INTEGER_OBJ {
		rightVal := r.(*object.Integer)
		r = &object.Float{Value: float64(rightVal.Value)}
	}
	return l, r
}

func isNumber(t object.ObjectType) bool {
	if t == object.INTEGER_OBJ || t == object.FLOAT_OBJ {
		return true
	}
	return false
}

func evalStringAndIntegerInfixExpression(operator string, left, right object.Object) object.Object {
	if operator == "*" {
		if left.Type() == object.STRING_OBJ {
			leftVal := left.(*object.String).Value
			rightVal := right.(*object.Integer).Value
			return &object.String{Value: strings.Repeat(leftVal, int(rightVal))}
		} else {
			leftVal := left.(*object.Integer).Value
			rightVal := right.(*object.String).Value
			return &object.String{Value: strings.Repeat(rightVal, int(leftVal))}
		}
	} else {
		return newError("Unknown operator '%s' in: %s %s %s", operator, left.Type(), operator, right.Type())
	}
}

func evalStringInfixExpression(operator string, left, right object.Object) object.Object {
	if operator != "+" {
		return newError("Unknown operator '%s' in: %s %s %s", operator, left.Type(), operator, right.Type())
	}

	leftVal := left.(*object.String).Value
	rightVal := right.(*object.String).Value

	return &object.String{Value: leftVal + rightVal}
}

func evalBooleanInfixExpression(operator string, left, right object.Object) object.Object {
	switch operator {
	case "==":
		return nativeBoolToBooleanObject(left == right)
	case "!=":
		return nativeBoolToBooleanObject(left != right)
	default:
		return newError("Unknown operator '%s' in: %s %s %s", operator, left.Type(), operator, right.Type())
	}
}

func evalFloatInfixExpression(operator string, left, right object.Object) object.Object {
	leftVal := left.(*object.Float).Value
	rightVal := right.(*object.Float).Value
	switch operator {
	case "+":
		return &object.Float{Value: leftVal + rightVal}
	case "-":
		return &object.Float{Value: leftVal - rightVal}
	case "*":
		return &object.Float{Value: leftVal * rightVal}
	case "/":
		return &object.Float{Value: leftVal / rightVal}
	case "<":
		return nativeBoolToBooleanObject(leftVal < rightVal)
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator '%s' in infix expression: %s %s %s", operator, left.Type(), operator, right.Type())
	}
}
func evalIntegerInfixExpression(operator string, left, right object.Object) object.Object {
	leftVal := left.(*object.Integer).Value
	rightVal := right.(*object.Integer).Value
	switch operator {
	case "+":
		return &object.Integer{Value: leftVal + rightVal}
	case "-":
		return &object.Integer{Value: leftVal - rightVal}
	case "*":
		return &object.Integer{Value: leftVal * rightVal}
	case "/":
		return &object.Integer{Value: leftVal / rightVal}
	case "<":
		return nativeBoolToBooleanObject(leftVal < rightVal)
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator '%s' in infix expression: %s %s %s", operator, left.Type(), operator, right.Type())
	}
}

func evalPrefixExpression(operator string, right object.Object) object.Object {
	switch operator {
	case "!":
		return evalBangOperatorExpression(right)
	case "-":
		return evalMinusPrefixOperatorExpression(right)
	default:
		return newError("unknown operator: %s%s", operator, right.Type())
	}
}

func evalMinusPrefixOperatorExpression(right object.Object) object.Object {
	if right.Type() != object.INTEGER_OBJ {
		return newError("Cannot apply minus prefix operator to this type: -%s", right.Type())
	}

	value := right.(*object.Integer).Value
	return &object.Integer{Value: -value}
}

func evalBangOperatorExpression(right object.Object) object.Object {
	switch right {
	case TRUE:
		return FALSE
	case FALSE:
		return TRUE
	case NULL:
		return TRUE
	default:
		return FALSE
	}
}

func nativeBoolToBooleanObject(input bool) *object.Boolean {
	if input {
		return TRUE
	}
	return FALSE
}

func evalProgram(stmts []ast.Statement, env *object.Environment) object.Object {
	var result object.Object

	for _, statement := range stmts {
		result = Eval(statement, env)

		switch result := result.(type) {
		case *object.ReturnValue:
			return result.Value
		case *object.Error:
			return result
		}
	}

	return result
}
