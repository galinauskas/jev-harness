// Package redact removes known credentials before persistence or transmission.
package redact

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type Redactor struct{ secrets []string }

func New(values ...string) Redactor {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		if strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") || strings.Contains(name, "PASSWORD") || strings.HasSuffix(name, "_KEY") {
			values = append(values, value)
		}
	}
	var secrets []string
	for _, v := range values {
		if len(v) >= 8 {
			secrets = append(secrets, v)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return Redactor{secrets}
}
func (r Redactor) Text(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
	}
	return s
}

func (r Redactor) JSON(data json.RawMessage) json.RawMessage {
	var v any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		return nil
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return r.Text(x)
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			for k, val := range x {
				x[k] = walk(val)
			}
		}
		return v
	}
	out, err := json.Marshal(walk(v))
	if err != nil {
		return nil
	}
	return out
}
