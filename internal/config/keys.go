package config

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"sync"
)

type configField struct {
	key  string
	name string
}

var documentFields = sync.OnceValue(func() []configField {
	fields := collectFields(reflect.TypeFor[document](), "")
	slices.SortFunc(fields, func(a, b configField) int {
		return cmp.Compare(a.key, b.key)
	})
	return fields
})

// Keys returns the configuration keys.
// Each key is the dotted JSON name of one leaf in the config document.
func Keys() []string {
	fields := documentFields()
	keys := make([]string, len(fields))
	for i, field := range fields {
		keys[i] = field.key
	}
	return keys
}

func fieldByKey(key string) (configField, bool) {
	for _, field := range documentFields() {
		if field.key == key {
			return field, true
		}
	}
	return configField{}, false
}

func keyByName(name string) string {
	for _, field := range documentFields() {
		if field.name == name {
			return field.key
		}
	}
	return ""
}

func collectFields(typ reflect.Type, prefix string) []configField {
	typ, ok := structType(typ)
	if !ok {
		return nil
	}
	var fields []configField
	for field := range typ.Fields() {
		fields = append(fields, fieldsOf(field, prefix)...)
	}
	return fields
}

func fieldsOf(field reflect.StructField, prefix string) []configField {
	name, ok := jsonName(field)
	if !ok {
		return nil
	}
	key := joinKey(prefix, name)
	if _, nested := structType(field.Type); nested {
		return collectFields(field.Type, key)
	}
	return []configField{{key: key, name: field.Name}}
}

func jsonName(field reflect.StructField) (string, bool) {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return "", false
	}
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return "", false
	}
	if name == "-" {
		return "", false
	}
	return name, true
}

func structType(typ reflect.Type) (reflect.Type, bool) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, false
	}
	return typ, true
}

func joinKey(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
