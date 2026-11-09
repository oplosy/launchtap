package apiserver

import (
	"encoding/json"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// nullableObject marshals as JSON null when empty and as the object otherwise. huma refuses
// `nullable:"true"` on object references, so the schema is spelled out as oneOf [ref, null]
// to keep the published contract truthful for generated clients.
type nullableObject[T any] struct{ V *T }

func (n nullableObject[T]) MarshalJSON() ([]byte, error) {
	if n.V == nil {
		return []byte("null"), nil
	}
	return json.Marshal(n.V)
}

func (nullableObject[T]) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{OneOf: []*huma.Schema{
		r.Schema(reflect.TypeFor[T](), true, ""),
		{Type: "null"},
	}}
}
