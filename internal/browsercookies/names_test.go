package browsercookies

import "strings"

func headerNames(header string) []string {
	var out []string
	for _, part := range strings.Split(header, ";") {
		name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
