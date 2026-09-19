package utils

import (
	"bolt/object"
	"fmt"
	"math"
	"strings"
)

var (
	NULL  = &object.Null{}
	TRUE  = &object.Boolean{Value: true}
	FALSE = &object.Boolean{Value: false}
)

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
func nativeBoolToBooleanObject(input bool) *object.Boolean {
	if input {
		return TRUE
	}
	return FALSE
}
func newError(format string, a ...interface{}) *object.Error {
	return &object.Error{Message: fmt.Sprintf(format, a...)}
}
func EvalInfixExpression(operator string, left, right object.Object, line int) object.Object {
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
	case left.Type() == object.NULL_OBJ || right.Type() == object.NULL_OBJ && (operator == "==" || operator == "!="):
		if operator == "==" {
			return nativeBoolToBooleanObject(left.Type() == right.Type())
		} else {
			return nativeBoolToBooleanObject(left.Type() != right.Type())
		}
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

	leftVal := left.(*object.String).Value
	rightVal := right.(*object.String).Value
	switch operator {
	case "+":
		return &object.String{Value: leftVal + rightVal}
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("Unknown operator '%s' in: %s %s %s - line=%d", operator, left.Type(), operator, right.Type(), line)
	}
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
		val := float64(leftVal) / float64(rightVal)
		if val == math.Trunc(val) {
			return &object.Integer{Value: leftVal / rightVal}
		}
		return &object.Float{Value: val}
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
