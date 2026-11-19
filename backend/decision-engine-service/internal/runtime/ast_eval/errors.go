package ast_eval

import "fmt"

type NullOperandError struct {
	Function string
	Operand  string
}

func (e *NullOperandError) Error() string {
	return fmt.Sprintf("%s %s is null", e.Function, e.Operand)
}

func nullOperandError(function, operand string, value any) error {
	if value == nil {
		return &NullOperandError{Function: function, Operand: operand}
	}
	return nil
}
