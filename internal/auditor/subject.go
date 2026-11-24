package auditor

import "strings"

// subjectOf reads the first non-empty line of a commit message.
func subjectOf(message string) string {
	for line := range strings.SplitSeq(message, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return strings.TrimSpace(line)
	}
	return ""
}
