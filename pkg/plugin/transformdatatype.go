package plugin

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

func GetDataTypeByType(dataType reflect.Type) string {
	if dataType.AssignableTo(reflect.TypeOf(int64(0))) {
		return "int64"
	} else if dataType.AssignableTo(reflect.TypeOf(float64(0))) {
		return "float64"
	} else if (dataType.AssignableTo(reflect.TypeOf(time.Time{}))) {
		return "time"
	} else {
		return "string"
	}
}

func ConvertNativeValue(val interface{}, dataType string) any {
	if val == nil {
		return nil
	}
	switch dataType {
	case "int64":
		switch v := val.(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		case string:
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		case []byte:
			n, _ := strconv.ParseInt(string(v), 10, 64)
			return n
		default:
			n, _ := strconv.ParseInt(fmt.Sprintf("%v", v), 10, 64)
			return n
		}
	case "float64":
		switch v := val.(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		case string:
			f, _ := strconv.ParseFloat(v, 64)
			return f
		case []byte:
			f, _ := strconv.ParseFloat(string(v), 64)
			return f
		default:
			f, _ := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
			return f
		}
	case "time":
		switch v := val.(type) {
		case time.Time:
			return v
		case string:
			t, _ := time.Parse(time.RFC3339, v)
			return t
		case []byte:
			t, _ := time.Parse(time.RFC3339, string(v))
			return t
		default:
			return time.Time{}
		}
	default:
		switch v := val.(type) {
		case string:
			return v
		case []byte:
			return string(v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
}

func ConvertValueArray(dataType string, sourceValues []any) any {
	var values any
	if dataType == "int64" {
		values = ConvertSlice[int64](sourceValues, 0)
	} else if dataType == "float64" {
		values = ConvertSlice[float64](sourceValues, 0)
	} else if dataType == "time" {
		values = ConvertSlice[time.Time](sourceValues, time.Time{}.Local())
	} else {
		values = ConvertSlice[string](sourceValues, "")
	}
	return values
}

func ConvertSlice[E any](in []any, nilValue E) (out []E) {
	out = make([]E, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, v.(E))
		} else {
			out = append(out, nilValue)
		}
	}
	return out
}
