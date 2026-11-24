package config

import (
	json "encoding/json/v2"
	"fmt"
)

// ParseSetValue reads a JSON array of command arguments.
func ParseSetValue(key, text string) ([]string, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	return parseJSONList(key, text)
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
