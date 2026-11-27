package config

import "fmt"

const commandKey = "auditor.validate.command"

// Keys returns the recognized configuration keys.
func Keys() []string {
	return []string{commandKey}
}

func checkKey(key string) error {
	if key == commandKey {
		return nil
	}
	return fmt.Errorf("unknown config key %q", key)
}
