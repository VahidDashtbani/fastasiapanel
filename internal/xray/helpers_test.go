package xray

import "encoding/json"

func jsonMarshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return b, err
}
