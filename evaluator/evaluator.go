package evaluator

import (
	"bolt/ast"
	"bolt/object"
	"bolt/utils"
	"fmt"
	"os"
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

func isBreak(obj object.Object) bool {
	if obj != nil {
		return obj.Type() == object.BREAK_OBJ
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
	case *ast.StructType:
		evalStructStatement(node, env)
		break
	case *ast.PubStatement:
		env.Exporting = true
		res := Eval(node.Stmt, env)
		if isError(res) {
			return res
		}
		env.Exporting = false
	case *ast.ImportStatement:
		// Find the new module
		// If it exists, create new module
		// lex then parse it
		// evaluate in the Module environment
		// get exports
		// add exports to current environment
		if !strings.HasSuffix(node.Value, ".bolt") {
			return newError("Import must be a .bolt file - line=%d", node.Line())
		}
		if !utils.FileExists(node.Value) {
			return newError("Cannot find import: %s - line=%d", node.Value, node.Line())
		}
		path, _ := os.Stat(node.Value)
		modulename := utils.FileName(path.Name())
		fmt.Println(modulename)
		file := utils.ReadFile(path.Name())
		program, module := utils.ProcessModule(file)
		if program == nil {
			return newError("Import file had errors - line=%d", node.Line())
		}
		res := Eval(program, module.Env)
		if isError(res) {
			err := res.(*object.Error)
			err.Message = "IMPORT " + modulename + ".bolt: " + err.Message
			return err
		}
		fmt.Println(res)
		moduleStruct := &object.StructInstance{}
		fields := map[string]object.Object{}
		for export, value := range module.Env.Exports {
			fields[export] = value
		}
		moduleStruct.Fields = fields
		env.Set(modulename, moduleStruct)
	case *ast.BreakStatement:
		if env.InFor {
			return &object.Break{}
		} else {
			return newError("Cannot use break statement when not in for loop - line=%d", node.Line())
		}
	case *ast.Boolean:
		return nativeBoolToBooleanObject(node.Value)
	case *ast.PrefixExpression:
		right := Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalPrefixExpression(node.Operator, right, node.Line())
	case *ast.InfixExpression:
		left := Eval(node.Left, env)
		right := Eval(node.Right, env)
		if isError(left) {
			return left
		}
		if isError(right) {
			return right
		}
		return evalInfixExpression(node.Operator, left, right, node.Line())
	case *ast.BlockStatement:
		return evalBlockStatement(node, env, false)
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
		if env.Exporting {
			env.Exports[node.Name.Value] = val
		}
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env)
	case *ast.FunctionLiteral:
		params := node.Parameters
		body := node.Body
		return &object.Function{Parameters: params, Body: body, Env: env}
	case *ast.ForExpression:
		return evalForExpression(node, env)
	case *ast.FieldAccess:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		return evalFieldAccess(left, node.Right, env)
	case *ast.CallExpression:
		function := Eval(node.Function, env)
		if isError(function) {
			return function
		}
		args := evalExpressions(node.Arguments, env)
		if len(args) == 1 && isError(args[0]) {
			return args[0]
		}
		return applyFunction(function, args, node.Line())
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
		return evalIndexExpression(left, index, node.Line())
	case *ast.StructInstantiation:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		return evalStructInstantiation(left, node, env)
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
		return evalSliceExpression(left, firstInd, secondInd, node.Line())
	}
	return nil
}

func evalFieldAccess(left object.Object, right ast.Expression, env *object.Environment) object.Object {
	switch left := left.(type) {
	case *object.StructInstance:
		if r, ok := right.(*ast.Identifier); ok {
			value, ok := left.Fields[r.Value]
			if !ok {
				return newError("Struct %s Has no field %s - line=%d", left.Type(), right.String(), right.Line())
			}
			return value
		} else if r, ok := right.(*ast.CallExpression); ok {
			value, ok := left.Fields[r.Function.(*ast.Identifier).Value]
			if !ok {
				return newError("Struct %s Has no function field %s - line=%d", left.Type(), right.String(), right.Line())
			}
			args := evalExpressions(r.Arguments, env)
			if len(args) == 1 && isError(args[0]) {
				return args[0]
			}
			return applyFunction(value, args, right.Line())
		} else if r, ok := right.(*ast.FieldAccess); ok {
			return evalFieldAccess(left.Fields[r.Left.String()], r.Right, env)
		} else {
			return newError("Cannot use non identifier to access struct field. Got=%s - line=%d", right, right.Line())
		}
	default:
		return newError("Cannot use field access on %s - line=%d", left.Type(), right.Line())
	}
}

func evalStructInstantiation(left object.Object, node *ast.StructInstantiation, env *object.Environment) object.Object {
	if left.Type() != object.STRUCT_OBJ {
		return newError("Cannot instantiate struct from %s since %s is not a type - line=%d", left.Type(), left.Type(), node.Line())
	}
	st := &object.StructInstance{Fields: map[string]object.Object{}}
	for f, val := range node.Fields {
		value := Eval(val, env)
		if isError(value) {
			return value
		}
		st.Fields[f] = value
	}
	return st
}

func evalStructStatement(node *ast.StructType, env *object.Environment) object.Object {
	st := &object.StructType{Name: node.Name, Fields: node.Fields}
	if env.Exporting {
		env.Exports[st.Name.Value] = st
	}
	env.Set(st.Name.Value, st)
	return NULL
}

func evalForExpression(node *ast.ForExpression, env *object.Environment) object.Object {
	forEnv := object.NewEnclosedEnvironment(env, true)
	ranging := false
	secondVal := false
	var rang *ast.RangeExpression = nil
	if ran, ok := node.Condition.(*ast.RangeExpression); ok {
		ranging = true
		rang = ran
		forEnv.Set(ran.Val1.Token.Literal, &object.Integer{})
		if ran.Val2 != nil {
			secondVal = true
			forEnv.Set(ran.Val2.Token.Literal, nil)
		}
	}
	if !ranging {
		for {
			cond := Eval(node.Condition, forEnv)
			if isError(cond) {
				return cond
			}
			if isTruthy(cond) {
				conseq := evalBlockStatement(node.Consequence, forEnv, true)
				if isError(conseq) {
					return conseq
				}
				if isBreak(conseq) {
					return NULL
				}
			} else {
				break
			}
		}
	} else {
		r := Eval(rang.Ranging, env)
		if isError(r) {
			return r
		}
		switch r.(type) {
		case *object.Array:
			arr := r.(*object.Array)
			for x, y := range arr.Elements {
				forEnv.Set(rang.Val1.Token.Literal, &object.Integer{Value: int64(x)})
				if secondVal {
					forEnv.Set(rang.Val2.Token.Literal, y)
				}
				conseq := evalBlockStatement(node.Consequence, forEnv, true)
				if isError(conseq) {
					return conseq
				}
				if isBreak(conseq) {
					return NULL
				}
			}
		case *object.Hash:
			dict := r.(*object.Hash)
			for _, pair := range dict.Pairs {
				forEnv.Set(rang.Val1.Token.Literal, pair.Key)
				if secondVal {
					forEnv.Set(rang.Val2.Token.Literal, pair.Value)
				}
				conseq := evalBlockStatement(node.Consequence, forEnv, true)
				if isError(conseq) {
					return conseq
				}
				if isBreak(conseq) {
					return NULL
				}
			}
		case *object.String:
			str := r.(*object.String)
			for x, y := range str.Value {
				forEnv.Set(rang.Val1.Token.Literal, &object.Integer{Value: int64(x)})
				if secondVal {
					forEnv.Set(rang.Val2.Token.Literal, &object.String{Value: string(y)})
				}
				conseq := evalBlockStatement(node.Consequence, forEnv, true)
				if isError(conseq) {
					return conseq
				}
				if isBreak(conseq) {
					return NULL
				}
			}
		}
	}
	return NULL
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
			return newError("Unusable as hash key: %s - line=%d", key.Type(), node.Line())
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

func evalSliceExpression(left, firstInd, secondInd object.Object, line int) object.Object {
	if left.Type() != object.ARRAY_OBJ {
		return newError("Array slicing operator not supported on: %s - line=%d", left.Type(), line)
	}

	if firstInd.Type() == object.INTEGER_OBJ && secondInd.Type() == object.INTEGER_OBJ {
		arrObj := left.(*object.Array)
		firstIndObj := firstInd.(*object.Integer)
		secondIndObj := secondInd.(*object.Integer)
		if int(firstIndObj.Value) <= len(arrObj.Elements) && firstIndObj.Value >= 0 && int(secondIndObj.Value) <= len(arrObj.Elements) && secondIndObj.Value >= 0 {
			return &object.Array{Elements: arrObj.Elements[firstIndObj.Value:secondIndObj.Value]}
		} else {
			return newError("Attempting to access index outside of array bounds %s[%d:%d] - line=%d", left.Type(), firstIndObj.Value, secondIndObj.Value, line)
		}
	} else {
		return newError("Values in array slicing expression must be integer %s[%s:%s] - line=%d", left.Type(), firstInd.Type(), secondInd.Type(), line)
	}
}

func evalHashIndexExpression(left, index object.Object, line int) object.Object {
	hashObject := left.(*object.Hash)

	key, ok := index.(object.Hashable)
	if !ok {
		return newError("Unusable as hash key: %s - line=%d", index.Type(), line)
	}

	pair, ok := hashObject.Pairs[key.HashKey()]
	if !ok {
		return NULL
	}

	return pair.Value
}

func evalIndexExpression(left, index object.Object, line int) object.Object {
	switch {
	case left.Type() == object.ARRAY_OBJ && index.Type() == object.INTEGER_OBJ:
		return evalArrayIndexExpression(left, index)
	case left.Type() == object.HASH_OBJ:
		return evalHashIndexExpression(left, index, line)
	default:
		return newError("Index operator not supported: %s - line=%d", left.Type(), line)
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

func applyFunction(fn object.Object, args []object.Object, line int) object.Object {
	switch fn := fn.(type) {
	case *object.Function:
		if len(fn.Parameters) != len(args) {
			return newError("Function requires %d arguments, recieved %d - line=%d", len(fn.Parameters), len(args), line)
		}
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *object.Builtin:
		return fn.Fn(args...)
	default:
		return newError("Not a function: %s - line=%d", fn.Type(), line)
	}
}

func extendFunctionEnv(fn *object.Function, args []object.Object) *object.Environment {
	env := object.NewEnclosedEnvironment(fn.Env, false)
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
	if node.Value == "_" {
		return newError("Cannot use blank identifier _ as value or type - line=%d", node.Line())
	}
	if val, ok := env.Get(node.Value); ok {
		return val
	}

	if builtin, ok := builtins[node.Value]; ok {
		return builtin
	}

	return newError("Identifier not found: %s - line=%d", node.Value, node.Line())
}

// In the evalProgram, nested returns wouldn't work. This is because it only stops for a return if the current statement type is a return value. This is a problem because eva:Program could be on a if expression for example and even if that if statement has a return inside that returns something it wouldn't use that value since it has been given the return value and not the return type. This means that when it checks if the value is a statement, it will be false since the return value isnt a return statement but an expression like an integer or boolean. However, with this implementation, if we are on an if statement with a return inside, when the return is evaluated, the value isn't passed back up, the statement is. This therefore means that it does register that there is a return block since it is given a whole statement.
func evalBlockStatement(block *ast.BlockStatement, env *object.Environment, inFor bool) object.Object {
	var result object.Object

	for _, statement := range block.Statements {
		result = Eval(statement, env)

		if result != nil {
			rt := result.Type()
			if rt == object.RETURN_VALUE_OBJ || rt == object.ERROR_OBJ || (inFor && rt == object.BREAK_OBJ) {
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
		return Eval(ie.Consequence, object.NewEnclosedEnvironment(env, false))
	} else if ie.Alternative != nil {
		return Eval(ie.Alternative, object.NewEnclosedEnvironment(env, false))
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

func evalInfixExpression(operator string, left, right object.Object, line int) object.Object {
	if left == nil || right == nil {
		if left == nil {
			return newError("Unknown left side of infix expression: nil - line=%d", line)
		}
		if right == nil {
			return newError("Unknown right side of infix expression: nil - line=%d", line)
		}
	}

	if operator == "&&" || operator == "||" {
		return evalBooleanOperatorInfixExpression(operator, left, right, line)
	}

	switch {
	case left.Type() == object.INTEGER_OBJ && right.Type() == object.INTEGER_OBJ:
		return evalIntegerInfixExpression(operator, left, right, line)
	case isNumber(left.Type()) && isNumber(right.Type()):
		l, r := convertInfixNumsToFloat(left, right)
		return evalFloatInfixExpression(operator, l, r, line)
	case left.Type() == object.BOOLEAN_OBJ && right.Type() == object.BOOLEAN_OBJ:
		return evalBooleanInfixExpression(operator, left, right, line)
	case left.Type() == object.STRING_OBJ && right.Type() == object.STRING_OBJ:
		return evalStringInfixExpression(operator, left, right, line)
	case (left.Type() == object.STRING_OBJ && right.Type() == object.INTEGER_OBJ) || (left.Type() == object.INTEGER_OBJ && right.Type() == object.STRING_OBJ):
		return evalStringAndIntegerInfixExpression(operator, left, right, line)
	case left.Type() != right.Type():
		return newError("Type mismatch on infix expression: %s %s %s - line=%d", left.Type(), operator, right.Type(), line)
	default:
		return newError("Unknown operator '%s' in: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
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

func evalStringAndIntegerInfixExpression(operator string, left, right object.Object, line int) object.Object {
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
		return newError("Unknown operator '%s' in: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
	}
}

func evalStringInfixExpression(operator string, left, right object.Object, line int) object.Object {
	if operator != "+" {
		return newError("Unknown operator '%s' in: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
	}

	leftVal := left.(*object.String).Value
	rightVal := right.(*object.String).Value

	return &object.String{Value: leftVal + rightVal}
}

func evalBooleanOperatorInfixExpression(operator string, left, right object.Object, line int) object.Object {
	switch operator {
	case "&&":
		return nativeBoolToBooleanObject(isTruthy(left) && isTruthy(right))
	case "||":
		return nativeBoolToBooleanObject(isTruthy(left) || isTruthy(right))
	default:
		return newError("Unknown operator '%s' in: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
	}
}
func evalBooleanInfixExpression(operator string, left, right object.Object, line int) object.Object {
	switch operator {
	case "==":
		return nativeBoolToBooleanObject(left == right)
	case "!=":
		return nativeBoolToBooleanObject(left != right)
	default:
		return newError("Unknown operator '%s' in: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
	}
}

func evalFloatInfixExpression(operator string, left, right object.Object, line int) object.Object {
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
	case "<=":
		return nativeBoolToBooleanObject(leftVal <= rightVal)
	case ">=":
		return nativeBoolToBooleanObject(leftVal >= rightVal)
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator '%s' in infix expression: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
	}
}
func evalIntegerInfixExpression(operator string, left, right object.Object, line int) object.Object {
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
	case "<=":
		return nativeBoolToBooleanObject(leftVal <= rightVal)
	case ">=":
		return nativeBoolToBooleanObject(leftVal >= rightVal)
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator '%s' in infix expression: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
	}
}

func evalPrefixExpression(operator string, right object.Object, line int) object.Object {
	switch operator {
	case "!":
		return evalBangOperatorExpression(right)
	case "-":
		return evalMinusPrefixOperatorExpression(right, line)
	default:
		return newError("unknown operator: %s%s - line=%d", operator, right.Type(), line)
	}
}

func evalMinusPrefixOperatorExpression(right object.Object, line int) object.Object {
	if right.Type() != object.INTEGER_OBJ {
		return newError("Cannot apply minus prefix operator to this type: -%s - line=%d", right.Type(), line)
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
