package config

import (
	json "encoding/json/v2"
	"fmt"
)

// ParseSetValue reads the JSON text for one config set value.
// A list key requires a JSON array of strings.
// A scalar key requires a JSON string.
func ParseSetValue(key, text string) ([]string, error) {
	kind, err := kindOf(key)
	if err != nil {
		return nil, err
	}
	switch kind {
	case kindCommand:
		return parseJSONList(key, text)
	case kindMode:
		return parseJSONScalar(key, text)
	default:
		return nil, fmt.Errorf("unknown config key %q", key)
	}
}

func parseJSONList(key, text string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil, fmt.Errorf("config key %q needs a JSON array of strings: %w", key, err)
	}
	if values == nil {
		return nil, fmt.Errorf("config key %q needs a JSON array of strings", key)
	}
	return values, nil
}

func parseJSONScalar(key, text string) ([]string, error) {
	var value string
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return nil, fmt.Errorf("config key %q needs a JSON string: %w", key, err)
	}
	return []string{value}, nil
}
