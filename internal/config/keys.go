package config

import (
	"fmt"
	"strings"
)

const commandKey = "auditor.validate.command"

// Keys returns the recognized configuration keys.
func Keys() []string {
	return []string{commandKey}
}

func checkKey(key string) error {
	switch {
	case key == commandKey:
		return nil
	case key == "auditor.verify", strings.HasPrefix(key, "auditor.verify."):
		return fmt.Errorf("config key %q was renamed; use auditor.validate instead of auditor.verify", key)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
}
