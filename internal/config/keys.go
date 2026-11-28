package config

import (
	"fmt"
	"strings"
)

const commandKey = "validate.command"

// Keys returns the recognized configuration keys.
func Keys() []string {
	return []string{commandKey}
}

func checkKey(key string) error {
	switch {
	case key == commandKey:
		return nil
	case key == "auditor", strings.HasPrefix(key, "auditor."):
		return fmt.Errorf("config key %q was removed; use validate instead of auditor", key)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
}
