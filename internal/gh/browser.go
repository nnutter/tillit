package gh

import (
	"os/exec"
	"runtime"
)

// OpenURL opens rawURL in the user's browser.
// It is a variable so tests can substitute a fake.
var OpenURL = func(rawURL string) error {
	name, args := openCommand(runtime.GOOS, rawURL)
	return exec.Command(name, args...).Start()
}

func openCommand(system, rawURL string) (string, []string) {
	switch system {
	case "darwin":
		return "open", []string{rawURL}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		return "xdg-open", []string{rawURL}
	}
}
