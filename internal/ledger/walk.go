package ledger

import "reflect"

func reflectValue(v any) reflect.Value { return reflect.ValueOf(v) }

var versionType = reflect.TypeOf(Version{})

// walkHMAC reports whether the value contains a Version of kind hmac, looking
// through pointers, structs, slices, arrays and maps.
func walkHMAC(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Invalid:
		return false
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return false
		}
		return walkHMAC(v.Elem())
	case reflect.Struct:
		if v.Type() == versionType {
			return VersionKind(v.FieldByName("Kind").String()) == KindHMAC
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() && walkHMAC(v.Field(i)) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if walkHMAC(v.Index(i)) {
				return true
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if walkHMAC(v.MapIndex(k)) {
				return true
			}
		}
	}
	return false
}
