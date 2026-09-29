package model

import (
	"errors"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// validatorInstance mirrors pkg/response naming (JSON field names in messages).
var validatorInstance = func() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})
	return v
}()

// Validate runs struct validation and returns a fields map (nil when valid).
func Validate(v interface{}) map[string]string {
	if err := validatorInstance.Struct(v); err != nil {
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			fields := make(map[string]string, len(verrs))
			for _, fe := range verrs {
				fields[fe.Field()] = message(fe)
			}
			return fields
		}
		return map[string]string{"request": "is invalid"}
	}
	return nil
}

func message(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "max":
		if fe.Type().String() == "string" || strings.Contains(fe.Type().String(), "string") {
			return "must be at most " + fe.Param() + " characters"
		}
		return "must have at most " + fe.Param() + " items"
	case "oneof":
		return "must be one of: " + strings.Join(strings.Split(fe.Param(), " "), ", ")
	default:
		return "is invalid"
	}
}
