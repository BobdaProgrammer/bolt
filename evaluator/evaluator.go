package evaluator

import (
	"bolt/ast"
	"bolt/object"
	"bolt/std"
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
	case *ast.SwitchExpression:
		value := Eval(node.SwitchItem, env)
		if isError(value) {
			return value
		}
		for _, cas := range node.Cases {
			cond := Eval(cas.Condition, env)
			if isError(cond) {
				return cond
			}

			val := utils.EvalInfixExpression("==", value, cond, cas.Line(), TRUE, FALSE, NULL)
			if isError(val) {
				return newError("Cannot use %s as a case in switch that is comparing %s - line=%d", cond.Type(), value.Type(), cas.LineNum)
			} else if b, ok := val.(*object.Boolean); ok {
				if b.Value {
					return evalBlockStatement(cas.Consequence, env, false)
				}
			}

		}
	case *ast.Defer:
		object.AddDeferToEnv(node, env)
		break
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
			if std.IsStandardLib(node.Value) {
				program, module := std.LoadStdPkg(node.Value)
				if program == nil {
					return newError("Import file had errors - line=%d", node.Line())
				}
				res := Eval(program, module.Env)
				if isError(res) {
					err := res.(*object.Error)
					err.Message = "IMPORT " + node.Value + ".bolt: " + err.Message
					return err
				}
				moduleStruct := &object.StructInstance{}
				fields := map[string]object.Object{}
				for export, value := range module.Env.Exports {
					fields[export] = value
				}
				moduleStruct.Fields = fields
				env.Set(node.Value, moduleStruct)
				break
			} else {
				return newError("Import must be a .bolt file or package from standard library - line=%d", node.Line())
			}
		}
		if !utils.FileExists(node.Value) {
			return newError("Cannot find import: %s - line=%d", node.Value, node.Line())
		}
		path, _ := os.Stat(node.Value)
		modulename := utils.FileName(path.Name())
		file := utils.ReadFile(node.Value)
		program, module := utils.ProcessModule(file, false)
		if program == nil {
			return newError("Import file had errors - line=%d", node.Line())
		}
		res := Eval(program, module.Env)
		if isError(res) {
			err := res.(*object.Error)
			err.Message = "IMPORT " + modulename + ".bolt: " + err.Message
			return err
		}
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
	case *ast.ContinueStatement:
		if env.InFor {
			return &object.Continue{}
		} else {
			return newError("Cannot use continue statement when not in for loop - line=%d", node.Line())
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
		return utils.EvalInfixExpression(node.Operator, left, right, node.Line(), TRUE, FALSE, NULL)
	case *ast.BlockStatement:
		return evalBlockStatement(node, env, env.InFor)
	case *ast.IfExpression:
		return evalIfExpression(node, env)
	case *ast.Null:
		return NULL
	case *ast.ReturnStatement:
		if len(node.ReturnValues) == 0 {
			return &object.ReturnValue{Value: nil}
		}
		if len(node.ReturnValues) == 1 {
			val := Eval(node.ReturnValues[0], env)
			if isError(val) {
				return val
			}
			return &object.ReturnValue{Value: val}
		}
		vals := &object.Spread{Elements: []object.Object{}}
		for _, v := range node.ReturnValues {
			val := Eval(v, env)
			if isError(val) {
				return val
			}
			vals.Elements = append(vals.Elements, val)
		}
		return &object.ReturnValue{Value: vals}
	case *ast.AssignExpression:
		val := Eval(node.Value, env)
		if isError(val) {
			return val
		}
		switch left := node.Left.(type) {
		case *ast.Identifier:
			if _, ok := env.Get(left.Value); !ok {
				return newError("Cannot use uninitialised variable in non let statement assignment: %s - line=%d", left.Value, node.Line())
			}
			if env.Exporting {
				env.Exports[left.Value] = val
			}
			env.AssignSet(left.Value, val)
		case *ast.FieldAccess:
			if env.Exporting {
				return newError("Cannot use pub on field access assignment. Use pub on the struct instance instead - line=%d", node.Line())
			}
			l := Eval(left.Left, env)
			if isError(l) {
				return l
			}
			ev := evalFieldAccessAssignment(l, left.Right, val, env, left)
			if isError(ev) {
				return ev
			}
		case *ast.ArrayLiteral:

			if val.Type() != object.SPREAD_OBJ {
				return newError("Cannot use multiple value let statement without multiple values. line=%d", node.Line())
			}
			spread := val.(*object.Spread)
			if len(left.Elements) > len(spread.Elements) {
				return newError("Too many values on left side of let statement for multiple return values. Wanted %d, got %d - line=%d", len(spread.Elements), len(left.Elements))
			}
			for i, el := range left.Elements {
				if ident, ok := el.(*ast.Identifier); ok {

					if _, ok := env.Get(ident.Value); !ok {
						return newError("Cannot use uninitialised variable in non let statement assignment: %s - line=%d", ident.Value, node.Line())
					}
					env.AssignSet(ident.Value, spread.Elements[i])
				} else {
					return newError("Cannot use non identifier in multiple value let statement, line=%d", node.Line())
				}
			}
		}
	case *ast.LetStatement:
		val := Eval(node.Value, env)
		if isError(val) {
			return val
		}

		if l, ok := node.Left.(*ast.Identifier); ok {
			if env.Exporting {
				env.Exports[l.Value] = val
			}
			env.Set(l.Value, val)
		} else if fa, ok := node.Left.(*ast.FieldAccess); ok {
			if env.Exporting {
				return newError("Cannot use pub on field access assignment. Use pub on the struct instance instead - line=%d", node.Line())
			}
			left := Eval(fa.Left, env)
			if isError(left) {
				return left
			}
			ev := evalFieldAccessAssignment(left, fa.Right, val, env, fa)
			if isError(ev) {
				return ev
			}
		} else if arr, ok := node.Left.(*ast.ArrayLiteral); ok {
			if val.Type() != object.SPREAD_OBJ {
				return newError("Cannot use multiple value let statement without multiple values. line=%d", node.Line())
			}
			spread := val.(*object.Spread)
			if len(arr.Elements) > len(spread.Elements) {
				return newError("Too many values on left side of let statement for multiple return values. Wanted %d, got %d - line=%d", len(spread.Elements), len(arr.Elements))
			}
			for i, el := range arr.Elements {
				if ident, ok := el.(*ast.Identifier); ok {
					env.Set(ident.Value, spread.Elements[i])
				} else {
					return newError("Cannot use non identifier in multiple value let statement, line=%d", node.Line())
				}
			}
		}
	case *ast.StructTypeConversion:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		if left.Type() != object.STRUCT_INSTANCE_OBJ {
			return newError("Cannot use struct conversion on non struct type %s - line %d", left.Type(), node.Line())
		}
		l := left.(*object.StructInstance)
		right := Eval(node.StType, env)
		if isError(right) {
			return right
		}
		if right.Type() != object.STRUCT_OBJ {
			return newError("Must use struct type as converter in struct conversion, got=%s - line=%d", right.Type(), node.Line())
		}
		r := right.(*object.StructType)
		return evalStructTypeConversion(l, r)
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
		if env.IsStd && strings.HasPrefix(node.Function.String(), "GOSTDCALL") && len(node.Function.String()) > 9 {
			val := node.Function.String()[9:]
			args := evalExpressions(node.Arguments, env)
			if len(args) == 1 && isError(args[0]) {
				return args[0]
			}
			returnVal := std.StandardGoCallFunction(val, args)
			return returnVal
		}
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
		els := evalExpressions(node.Elements, env)
		if len(els) == 1 && isError(els[0]) {
			return els[0]
		}
		return &object.Array{Elements: els}
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
	case *ast.IndexAssignExpression:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		index := Eval(node.Index, env)
		if isError(index) {
			return index
		}
		value := Eval(node.Value, env)
		if isError(value) {
			return value
		}
		if val := evalIndexAssignExpression(left, index, value, node.Line()); isError(val) {
			return val
		}
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

func evalIndexAssignExpression(left, index, value object.Object, line int) object.Object {
	if left.Type() == object.ARRAY_OBJ && index.Type() == object.INTEGER_OBJ {
		l := left.(*object.Array)
		i := index.(*object.Integer)
		if i.Value >= 0 && i.Value < int64(len(l.Elements)) {
			if value.Type() == object.SPREAD_OBJ {
				spread := value.(*object.Spread)
				l.Elements = append(l.Elements[:i.Value], append(spread.Elements, l.Elements[i.Value+1:]...)...)
			} else {
				l.Elements[i.Value] = value
			}
		} else {
			return newError("Index out of range %d in %s with length %d - line=%d", i.Value, l.Type(), len(l.Elements), line)
		}
	} else if left.Type() == object.HASH_OBJ {
		hashable, ok := index.(object.Hashable)
		if !ok {
			return newError("Cannot use %s as hash key in index expression - line=%d", index.Inspect(), line)
		}
		key := hashable.HashKey()
		hash := left.(*object.Hash)
		hash.Pairs[key] = object.HashPair{Key: index, Value: value}
	} else if left.Type() == object.STRING_OBJ && index.Type() == object.INTEGER_OBJ && value.Type() == object.STRING_OBJ {
		l := left.(*object.String)
		i := index.(*object.Integer)
		v := value.(*object.String)
		if i.Value >= 0 && i.Value < int64(len(l.Value)) {
			l.Value = l.Value[0:i.Value] + v.Value + l.Value[i.Value+1:len(l.Value)]
		} else {
			return newError("Index out of range %d in %s with length %d - line=%d", i.Value, l.Type(), len(l.Value), line)
		}
	}
	return nil
}

func evalStructTypeConversion(left *object.StructInstance, right *object.StructType) object.Object {
	if len(left.Fields) > len(right.Fields) {
		return NULL
	}

	rightnames := map[string]bool{}
	for _, field := range right.Fields {
		rightnames[field.Value] = true
	}

	newStruct := &object.StructInstance{Fields: map[string]object.Object{}, StType: right}
	for name, val := range left.Fields {
		if _, ok := rightnames[name]; !ok {
			return NULL
		} else {
			newStruct.Fields[name] = val
		}
	}
	return newStruct
}

func evalFieldAccessAssignment(left object.Object, right ast.Expression, value object.Object, env *object.Environment, node *ast.FieldAccess) object.Object {
	switch left := left.(type) {
	case *object.StructInstance:
		if r, ok := right.(*ast.Identifier); ok {
			if left.StType == nil {
				return newError("Cannot use field access on %s - line=%d", node.Left.String(), right.Line())
			}
			availableFieldsArr := left.StType.Fields
			found := false
			for _, field := range availableFieldsArr {
				if field.Value == r.Value {
					found = true
				}
			}
			if !found {
				return newError("%s is not an available field of %s formed from struct type: %s - line=%d", r.Value, left.Type(), left.StType.Name, right.Line())
			}
			left.Fields[r.Value] = value
			return nil
		} else if r, ok := right.(*ast.FieldAccess); ok {
			return evalFieldAccessAssignment(left.Fields[r.Left.String()], r.Right, value, env, node)
		} else {
			return newError("Cannot use non identifier in field access on %s - line=%d", left.Type(), right.Line())
		}
	default:
		return newError("Cannot use field access on %s - line=%d", left.Type(), right.Line())
	}
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
		} else if r, ok := right.(*ast.IndexExpression); ok {
			value, ok := left.Fields[r.Left.String()]
			if !ok {
				return newError("Struct %s Has no array or map field %s - line=%d", left.Type(), right.String(), right.Line())
			}
			ind := Eval(r.Index, env)
			if isError(ind) {
				return ind
			}
			return evalArrayIndexExpression(value, ind)
		} else if r, ok := right.(*ast.IndexAssignExpression); ok {
			left, ok := left.Fields[r.Left.String()]
			if !ok {
				return newError("Struct %s Has no array or map field %s - line=%d", left.Type(), right.String(), right.Line())
			}
			index := Eval(r.Index, env)
			if isError(index) {
				return index
			}
			val := Eval(r.Value, env)
			if isError(val) {
				return val
			}
			return evalIndexAssignExpression(left, index, val, right.Line())
		} else if r, ok := right.(*ast.FieldAccess); ok {
			return evalFieldAccess(left.Fields[r.Left.String()], r.Right, env)
		} else {
			return newError("Cannot use non identifier to access struct field. Got=%s - line=%d", right, right.Line())
		}
	case *object.UserError:
		if iden, ok := right.(*ast.Identifier); ok {
			if iden.Value == "message" {
				return &object.String{Value: left.Message}
			}
		}
		return newError("type ERROR has no field or function: %s - line=%d", right.String(), right.Line())
	default:
		return newError("Cannot use field access on %s - line=%d", left.Type(), right.Line())
	}
}

func evalStructInstantiation(left object.Object, node *ast.StructInstantiation, env *object.Environment) object.Object {
	if left.Type() != object.STRUCT_OBJ {
		return newError("Cannot instantiate struct from %s since %s is not a type - line=%d", left.Type(), left.Type(), node.Line())
	}
	l := left.(*object.StructType)
	fields := map[string]bool{}
	for _, field := range l.Fields {
		fields[field.Value] = true
	}

	st := &object.StructInstance{Fields: map[string]object.Object{}, StType: l}
	for f, val := range node.Fields {
		if _, ok := fields[f]; !ok {
			return newError("Struct type %s has no field %s - line=%d", l.Name.Value, f, node.Line())
		}
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
	forEnv := object.NewEnclosedEnvironment(env, true, env.IsStd, object.FORENV)
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
				if ok, re := forConseqChecks(conseq); ok {
					return re
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
		case *object.Integer:
			int := r.(*object.Integer)
			if secondVal {
				return newError("Can only use one variable when looping over integer, got 2 - line=%d", node.Line())
			}
			for x := range int.Value {
				forEnv.Set(rang.Val1.Token.Literal, &object.Integer{Value: int64(x)})
				conseq := evalBlockStatement(node.Consequence, forEnv, true)

				if ok, re := forConseqChecks(conseq); ok {
					return re
				}

			}
		case *object.Array:
			arr := r.(*object.Array)
			for x, y := range arr.Elements {
				forEnv.Set(rang.Val1.Token.Literal, &object.Integer{Value: int64(x)})
				if secondVal {
					forEnv.Set(rang.Val2.Token.Literal, y)
				}
				conseq := evalBlockStatement(node.Consequence, forEnv, true)

				if ok, re := forConseqChecks(conseq); ok {
					return re
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
				if ok, re := forConseqChecks(conseq); ok {
					return re
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
				if ok, re := forConseqChecks(conseq); ok {
					return re
				}
			}
		}
	}
	return NULL
}

func forConseqChecks(conseq object.Object) (bool, object.Object) {
	if conseq != nil {
		if isError(conseq) {
			return true, conseq
		}
		if isBreak(conseq) {
			return true, NULL
		}
		if conseq.Type() == object.RETURN_VALUE_OBJ {
			return true, conseq
		}
	}
	return false, nil
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
	switch left.Type() {
	case object.ARRAY_OBJ:
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
	case object.STRING_OBJ:
		if firstInd.Type() == object.INTEGER_OBJ && secondInd.Type() == object.INTEGER_OBJ {
			strObj := left.(*object.String)
			firstIndObj := firstInd.(*object.Integer)
			secondIndObj := secondInd.(*object.Integer)
			if int(firstIndObj.Value) <= len(strObj.Value) && firstIndObj.Value >= 0 && int(secondIndObj.Value) <= len(strObj.Value) && secondIndObj.Value >= 0 {
				if firstIndObj.Value > secondIndObj.Value {
					return newError("First index in slice expression is larger than second %s[%d:%d] - line=%d", left.Type(), firstIndObj.Value, secondIndObj.Value, line)
				}
				return &object.String{Value: strObj.Value[firstIndObj.Value:secondIndObj.Value]}
			} else {
				return newError("Attempting to access index outside of string bounds %s[%d:%d] - line=%d", left.Type(), firstIndObj.Value, secondIndObj.Value, line)
			}
		} else {
			return newError("Values in array slicing expression must be integer %s[%s:%s] - line=%d", left.Type(), firstInd.Type(), secondInd.Type(), line)
		}
	default:
		return newError("Array slicing operator not supported on: %s - line=%d", left.Type(), line)
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
	case left.Type() == object.STRING_OBJ:
		str := left.(*object.String)
		idx := index.(*object.Integer).Value
		if idx < 0 || (int(idx) > len(str.Value)-1) {
			return NULL
		}
		return &object.String{Value: string(str.Value[idx])}
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
		//  Handle special go calling that is only allowed in standard lib
		if len(fn.Parameters) != len(args) {
			return newError("Function requires %d arguments, recieved %d - line=%d", len(fn.Parameters), len(args), line)
		}
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := Eval(fn.Body, extendedEnv)
		for i := range extendedEnv.Defers {
			def := extendedEnv.Defers[len(extendedEnv.Defers)-1-i]
			err := Eval(def, extendedEnv)
			if isError(err) {
				return err
			}
		}
		return unwrapReturnValue(evaluated)
	case *object.Builtin:
		return fn.Fn(args...)
	default:
		return newError("Not a function: %s - line=%d", fn.Type(), line)
	}
}

func extendFunctionEnv(fn *object.Function, args []object.Object) *object.Environment {
	env := object.NewEnclosedEnvironment(fn.Env, false, fn.Env.IsStd, object.FUNCTIONENV)
	for paramIdx, param := range fn.Parameters {
		env.Set(param.Value, args[paramIdx])
	}

	return env
}
func unwrapReturnValue(obj object.Object) object.Object {
	if isError(obj) {
		return obj
	}
	if returnValue, ok := obj.(*object.ReturnValue); ok {
		return returnValue.Value
	}
	return NULL
}

func evalExpressions(exps []ast.Expression, env *object.Environment) []object.Object {
	var result []object.Object

	for _, e := range exps {
		evaluated := Eval(e, env)
		if isError(evaluated) {
			return []object.Object{evaluated}
		}
		if evaluated.Type() == object.SPREAD_OBJ {
			spread := evaluated.(*object.Spread)
			for _, e := range spread.Elements {
				result = append(result, e)
			}
		} else {
			result = append(result, evaluated)
		}
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
		SetBuiltinLineNum(node.Line())
		return builtin
	}

	return newError("Identifier not found: %s - line=%d", node.Value, node.Line())
}

// In the evalProgram, nested returns wouldn't work. This is because it only stops for a return if the current statement type is a return value. This is a problem because eva:Program could be on a if expression for example and even if that if statement has a return inside that returns something it wouldn't use that value since it has been given the return value and not the return type. This means that when it checks if the value is a statement, it will be false since the return value isnt a return statement but an expression like an integer or boolean. However, with this implementation, if we are on an if statement with a return inside, when the return is evaluated, the value isn't passed back up, the statement is. This therefore means that it does register that there is a return block since it is given a whole statement.
func evalBlockStatement(block *ast.BlockStatement, env *object.Environment, inFor bool) object.Object {
	var result object.Object

	localEnv := env
	if inFor {
		localEnv.InFor = true
	}

	for _, statement := range block.Statements {
		result = Eval(statement, localEnv)

		if result != nil {
			rt := result.Type()
			if rt == object.RETURN_VALUE_OBJ || rt == object.ERROR_OBJ || (inFor && rt == object.BREAK_OBJ) || (inFor && rt == object.CONTINUE_OBJ) {
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
		return Eval(ie.Consequence, object.NewEnclosedEnvironment(env, env.InFor, env.IsStd, object.IFENV))
	} else if ie.Alternative != nil {
		return Eval(ie.Alternative, object.NewEnclosedEnvironment(env, env.InFor, env.IsStd, object.IFENV))
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

func evalPrefixExpression(operator string, right object.Object, line int) object.Object {
	switch operator {
	case "!":
		return evalBangOperatorExpression(right)
	case "-":
		return evalMinusPrefixOperatorExpression(right, line)
	case "...":
		return evalEllipsisPrefixOperatorExpression(right, line)
	default:
		return newError("unknown operator: %s%s - line=%d", operator, right.Type(), line)
	}
}

func evalEllipsisPrefixOperatorExpression(right object.Object, line int) object.Object {
	if right.Type() != object.ARRAY_OBJ {
		return newError("Cannot use ellipsis prefix operator on non-array type %s. line=%d", right.Type(), line)
	}
	arr := right.(*object.Array)
	return &object.Spread{Elements: arr.Elements}
}

func evalMinusPrefixOperatorExpression(right object.Object, line int) object.Object {
	switch right.Type() {
	case object.INTEGER_OBJ:
		value := right.(*object.Integer).Value
		return &object.Integer{Value: -value}
	case object.FLOAT_OBJ:
		value := right.(*object.Float).Value
		return &object.Float{Value: -value}
	default:
		return newError("Cannot apply minus prefix operator to this type: -%s - line=%d", right.Type(), line)
	}
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
			for i := range env.Defers {
				def := env.Defers[len(env.Defers)-1-i]
				err := Eval(def, env)
				if isError(err) {
					return err
				}
			}
			return result.Value
		case *object.Error:
			return result
		}
	}

	for i := range env.Defers {
		def := env.Defers[len(env.Defers)-1-i]
		err := Eval(def, env)
		if isError(err) {
			return err
		}
	}
	return result
}
