package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/Abshoor24/backend-go/internal/apperror"
)

// validatorInstance uses JSON field names in validation messages so clients
// see "email", not the Go struct field "Email".
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

// DecodeAndValidate parses a JSON body, enforces a size limit, rejects unknown
// fields and runs struct validation. It returns a structured apperror on any
// failure.
func DecodeAndValidate(w http.ResponseWriter, r *http.Request, dst interface{}, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return apperror.BadRequest("Request body is too large", err)
		}
		return apperror.Validation("Request body is not valid JSON")
	}
	if dec.More() {
		return apperror.Validation("Request body must contain a single JSON object")
	}

	if err := validatorInstance.Struct(dst); err != nil {
		return mapValidationError(err)
	}
	return nil
}

// mapValidationError converts validator errors into a structured fields map.
func mapValidationError(err error) error {
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		fields := make(map[string]string, len(verrs))
		for _, fe := range verrs {
			fields[fe.Field()] = validationMessage(fe)
		}
		return apperror.ValidationFields(fields)
	}
	return apperror.Validation("Invalid request")
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	default:
		return "is invalid"
	}
}
