package httpapi

import (
	"net/url"
	"strconv"
	"strings"
)

// ParseDownloads reads DOWNLOADS. Each item is step|button|label|url,
// and items are separated by ;;. Only http and https addresses are kept.
func ParseDownloads(raw string) []Download {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []Download{}
	}
	parts := strings.Split(raw, ";;")
	out := make([]Download, 0, len(parts))
	for _, part := range parts {
		fields := strings.Split(part, "|")
		if len(fields) != 4 {
			continue
		}
		step, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil || step < 1 {
			continue
		}
		button := strings.TrimSpace(fields[1])
		label := strings.TrimSpace(fields[2])
		rawURL := strings.TrimSpace(fields[3])
		parsed, err := url.Parse(rawURL)
		if err != nil || button == "" || label == "" {
			continue
		}
		if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		out = append(out, Download{Step: step, Button: button, Label: label, URL: rawURL})
	}
	return out
}
