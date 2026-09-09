// Package security provides SSRF protection, request validation and limits.
package security

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// BlockedHosts are never allowed as custom-provider targets.
var blockedHosts = []string{
	"169.254.169.254", // cloud metadata (AWS/GCP/Azure)
	"metadata.google.internal",
	"metadata.google",
	"instance-data",
}

// ValidateProviderURL validates a custom provider base URL.
// It allows http/https, allows localhost/LAN (needed for Ollama etc.),
// but blocks cloud-metadata endpoints, credentials in URL, and odd schemes.
// Set allowPrivate=false to forbid LAN targets (not default).
func ValidateProviderURL(raw string, allowPrivate bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("only http:// and https:// URLs are allowed")
	}
	if u.User != nil {
		return "", errors.New("credentials must not be embedded in the URL; use the API key field")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("URL must include a host")
	}
	lower := strings.ToLower(host)
	for _, b := range blockedHosts {
		if lower == b || strings.HasSuffix(lower, "."+b) {
			return "", fmt.Errorf("host %q is blocked (cloud metadata protection)", host)
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLinkLocalUnicast() {
			return "", fmt.Errorf("link-local IP %q is blocked", host)
		}
		// 169.254.0.0/16 blocked entirely
		if strings.HasPrefix(host, "169.254.") {
			return "", fmt.Errorf("link-local IP %q is blocked", host)
		}
		if !allowPrivate && isPrivateIP(ip) && !ip.IsLoopback() {
			return "", fmt.Errorf("private network host %q is not allowed", host)
		}
	} else {
		// Hostnames that resolve only to metadata are still caught at dial time
		// by policy; here we just block obvious suffixes.
		if strings.HasSuffix(lower, ".internal") && !allowPrivate {
			return "", fmt.Errorf("internal host %q is not allowed", host)
		}
	}
	// Strip fragment/query for storage; keep path.
	u.Fragment = ""
	u.RawQuery = ""
	out := strings.TrimRight(u.String(), "/")
	return out, nil
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		}
		return false
	}
	// IPv6 ULA fc00::/7
	if len(ip) >= 2 && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

// ValidateModelID ensures a model id is sane.
func ValidateModelID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("model id is required")
	}
	if len(id) > 256 {
		return errors.New("model id too long")
	}
	for _, r := range id {
		if r < 32 || r == 127 {
			return errors.New("model id contains control characters")
		}
	}
	return nil
}
