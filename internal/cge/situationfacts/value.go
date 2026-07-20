package situationfacts

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

type FactValueKind string

const (
	ValueBool       FactValueKind = "bool"
	ValueInt        FactValueKind = "int"
	ValuePermille   FactValueKind = "permille"
	ValueString     FactValueKind = "string"
	ValueTimestamp  FactValueKind = "timestamp"
	ValueDurationMS FactValueKind = "duration_ms"
	ValueStringSet  FactValueKind = "string_set"
	ValueStringList FactValueKind = "string_list"
	ValueRef        FactValueKind = "ref"
)

type FactValue struct {
	Kind FactValueKind

	BoolValue       bool
	IntValue        int64
	PermilleValue   int64
	StringValue     string
	TimestampValue  time.Time
	StringSetValue  []string
	StringListValue []string
	RefValue        string
}

func BoolFactValue(value bool) FactValue { return FactValue{Kind: ValueBool, BoolValue: value} }
func IntFactValue(value int64) FactValue { return FactValue{Kind: ValueInt, IntValue: value} }
func PermilleFactValue(value int64) FactValue {
	return FactValue{Kind: ValuePermille, PermilleValue: value}
}
func StringFactValue(value string) FactValue { return FactValue{Kind: ValueString, StringValue: value} }
func TimestampFactValue(value time.Time) FactValue {
	return FactValue{Kind: ValueTimestamp, TimestampValue: value.UTC().Round(0)}
}
func DurationMSFactValue(value int64) FactValue {
	return FactValue{Kind: ValueDurationMS, IntValue: value}
}
func StringSetFactValue(values []string) FactValue {
	return FactValue{Kind: ValueStringSet, StringSetValue: canonicalStrings(values)}
}
func StringListFactValue(values []string) FactValue {
	return FactValue{Kind: ValueStringList, StringListValue: append([]string(nil), values...)}
}
func RefFactValue(value string) FactValue { return FactValue{Kind: ValueRef, RefValue: value} }

func canonicalStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func validValueKind(value FactValueKind) bool {
	return value == ValueBool || value == ValueInt || value == ValuePermille || value == ValueString || value == ValueTimestamp || value == ValueDurationMS || value == ValueStringSet || value == ValueStringList || value == ValueRef
}

func (v FactValue) Clone() FactValue {
	out := v
	out.StringSetValue = append([]string(nil), v.StringSetValue...)
	out.StringListValue = append([]string(nil), v.StringListValue...)
	return out
}

func (v FactValue) Validate(maxString, maxSet int) error {
	if !validValueKind(v.Kind) {
		return ErrInvalidFactValue
	}
	if !v.onlyKindPayload() {
		return ErrInvalidFactValue
	}
	if v.Kind == ValuePermille && (v.PermilleValue < 0 || v.PermilleValue > 1000) {
		return ErrInvalidFactValue
	}
	if (v.Kind == ValueInt || v.Kind == ValueDurationMS) && v.IntValue < 0 {
		return ErrInvalidFactValue
	}
	if v.Kind == ValueTimestamp && v.TimestampValue.IsZero() || v.Kind == ValueTimestamp && v.TimestampValue.Location() != time.UTC {
		return ErrInvalidFactValue
	}
	if v.Kind == ValueString || v.Kind == ValueRef {
		if !validText(v.StringValueOrRef(), false, maxString) {
			return ErrInvalidFactValue
		}
	}
	if v.Kind == ValueStringSet {
		if len(v.StringSetValue) > maxSet {
			return ErrFactLimitReached
		}
		for i, value := range v.StringSetValue {
			if !validText(value, false, maxString) || i > 0 && v.StringSetValue[i-1] >= value {
				return ErrInvalidFactValue
			}
		}
	}
	if v.Kind == ValueStringList {
		if len(v.StringListValue) > maxSet {
			return ErrFactLimitReached
		}
		for _, value := range v.StringListValue {
			if !validText(value, false, maxString) {
				return ErrInvalidFactValue
			}
		}
	}
	return nil
}

func (v FactValue) onlyKindPayload() bool {
	switch v.Kind {
	case ValueBool:
		return v.IntValue == 0 && v.PermilleValue == 0 && v.StringValue == "" && v.TimestampValue.IsZero() && len(v.StringSetValue) == 0 && len(v.StringListValue) == 0 && v.RefValue == ""
	case ValueInt, ValueDurationMS:
		return !v.BoolValue && v.PermilleValue == 0 && v.StringValue == "" && v.TimestampValue.IsZero() && len(v.StringSetValue) == 0 && len(v.StringListValue) == 0 && v.RefValue == ""
	case ValuePermille:
		return !v.BoolValue && v.IntValue == 0 && v.StringValue == "" && v.TimestampValue.IsZero() && len(v.StringSetValue) == 0 && len(v.StringListValue) == 0 && v.RefValue == ""
	case ValueString:
		return !v.BoolValue && v.IntValue == 0 && v.PermilleValue == 0 && v.TimestampValue.IsZero() && len(v.StringSetValue) == 0 && len(v.StringListValue) == 0 && v.RefValue == ""
	case ValueTimestamp:
		return !v.BoolValue && v.IntValue == 0 && v.PermilleValue == 0 && v.StringValue == "" && len(v.StringSetValue) == 0 && len(v.StringListValue) == 0 && v.RefValue == ""
	case ValueStringSet:
		return !v.BoolValue && v.IntValue == 0 && v.PermilleValue == 0 && v.StringValue == "" && v.TimestampValue.IsZero() && len(v.StringListValue) == 0 && v.RefValue == ""
	case ValueStringList:
		return !v.BoolValue && v.IntValue == 0 && v.PermilleValue == 0 && v.StringValue == "" && v.TimestampValue.IsZero() && len(v.StringSetValue) == 0 && v.RefValue == ""
	case ValueRef:
		return !v.BoolValue && v.IntValue == 0 && v.PermilleValue == 0 && v.StringValue == "" && v.TimestampValue.IsZero() && len(v.StringSetValue) == 0 && len(v.StringListValue) == 0
	default:
		return false
	}
}

func (v FactValue) StringValueOrRef() string {
	if v.Kind == ValueRef {
		return v.RefValue
	}
	return v.StringValue
}

func (v FactValue) Canonical() string {
	switch v.Kind {
	case ValueBool:
		if v.BoolValue {
			return "bool:true"
		}
		return "bool:false"
	case ValueInt, ValueDurationMS:
		return string(v.Kind) + ":" + strconv.FormatInt(v.IntValue, 10)
	case ValuePermille:
		return "permille:" + strconv.FormatInt(v.PermilleValue, 10)
	case ValueString:
		return "string:" + v.StringValue
	case ValueRef:
		return "ref:" + v.RefValue
	case ValueTimestamp:
		return "timestamp:" + v.TimestampValue.UTC().Round(0).Format(time.RFC3339Nano)
	case ValueStringSet:
		return "string_set:" + joinValues(v.StringSetValue)
	case ValueStringList:
		return "string_list:" + joinValues(v.StringListValue)
	default:
		return string(v.Kind)
	}
}

func joinValues(values []string) string {
	var builder strings.Builder
	builder.Grow(2 + len(values)*8)
	builder.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(strconv.Quote(value))
	}
	builder.WriteByte(']')
	return builder.String()
}
