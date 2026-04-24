package executor

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// DangerousEnvVars are environment variable keys that must never be set by user tasks.
// Setting these could lead to privilege escalation, library injection, or security bypass.
var dangerousEnvVars = map[string]bool{
	"LD_PRELOAD":         true,
	"LD_LIBRARY_PATH":    true,
	"LD_AUDIT":           true,
	"LD_DEBUG":           true,
	"LD_ORIGIN":          true,
	"LD_HWCAP_MASK":      true,
	"LDSO_PRELOAD":       true,
	"PATH":               true, // PATH must go through SafePaths
	"BASH_ENV":           true,
	"ENV":                true,
	"CDPATH":             true,
	"GLOBIGNORE":         true,
	"IFS":                true,
	"FUNCNEST":           true,
	"SHELLOPTS":          true,
	"NODE_OPTIONS":       true,
	"PYTHONPATH":         true,
	"PYTHONSTARTUP":      true,
	"PERL5OPT":           true,
	"PERL5LIB":           true,
	"RUBYOPT":            true,
	"GEM_PATH":           true,
	"JAVA_TOOL_OPTIONS":  true,
	"_JAVA_OPTIONS":      true,
	"CLASSPATH":          true,
	"MANPATH":            true,
	"INFOPATH":           true,
	"XDG_CONFIG_HOME":    true,
	"XDG_DATA_HOME":      true,
	"HOSTALIASES":        true,
	"LOCALDOMAIN":        true,
	"RES_OPTIONS":        true,
	"EDITOR":             true,
	"VISUAL":             true,
	"DISPLAY":            true,
	"WAYLAND_DISPLAY":    true,
	"DBUS_SESSION_BUS_ADDRESS": true,
	"XAUTHORITY":         true,
	"SSH_AUTH_SOCK":      true,
	"SSH_AGENT_PID":      true,
	"GPG_AGENT_INFO":     true,
	"KRB5_CONFIG":        true,
	"KRB5CCNAME":         true,
	"SYSTEMD_EDITOR":     true,
	"PAGER":              true,
	"LESSOPEN":           true,
	"LESSCLOSE":          true,
	"HTTP_PROXY":         true,
	"HTTPS_PROXY":        true,
	"ALL_PROXY":          true,
	"NO_PROXY":           true,
	"http_proxy":         true,
	"https_proxy":        true,
	"all_proxy":          true,
	"no_proxy":           true,
}

// SafeEnvPrefixes are prefixes for env vars that are always allowed.
var safeEnvPrefixes = []string{
	"CRONPLUS_",  // app-specific vars
	"MY_",        // common user prefix
	"APP_",       // common user prefix
	"SERVICE_",   // common service prefix
	"TASK_",      // common task prefix
}

// SanitizeEnvVars filters dangerous environment variables.
// Returns the safe subset and a list of rejected keys.
func SanitizeEnvVars(envVars map[string]string) (safe map[string]string, rejected []string) {
	safe = make(map[string]string, len(envVars))
	for k, v := range envVars {
		upper := strings.ToUpper(k)
		if dangerousEnvVars[upper] {
			rejected = append(rejected, k)
			continue
		}
		// Allow vars with safe prefixes
		allowed := false
		for _, prefix := range safeEnvPrefixes {
			if strings.HasPrefix(upper, prefix) {
				allowed = true
				break
			}
		}
		// Block vars starting with LD_ (catch-all for linker injection)
		if strings.HasPrefix(upper, "LD_") {
			rejected = append(rejected, k)
			continue
		}
		// Allow common safe vars
		if !allowed {
			safeCommon := map[string]bool{
				"LANG": true, "LC_ALL": true, "LC_CTYPE": true, "LC_MESSAGES": true,
				"TZ": true, "TERM": true, "HOME": true, "USER": true, "LOGNAME": true,
				"SHELL": true, "MAIL": true, "TMPDIR": true, "TMP": true, "TEMP": true,
			}
			if !safeCommon[upper] {
				// Unknown var — allow but log (could be too restrictive to block all)
				// For now, allow unknown vars but reject dangerous patterns
				if strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "SECRET") ||
					strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PRIVATE_KEY") {
					rejected = append(rejected, k)
					continue
				}
			}
		}
		safe[k] = v
	}
	return safe, rejected
}

// DecodeCommand tries to base64-decode a command string.
// Falls back to the original string if it's not valid base64.
func DecodeCommand(cmd string) string {
	if cmd == "" {
		return cmd
	}
	decoded, err := base64.StdEncoding.DecodeString(cmd)
	if err != nil {
		return cmd
	}
	// Verify roundtrip
	if base64.StdEncoding.EncodeToString(decoded) == cmd {
		return string(decoded)
	}
	return cmd
}

// NewRunID generates a short human-readable run identifier.
// Format: YYMMDD-HHMMSS-xxxx (4 random hex chars)
func NewRunID() string {
	now := time.Now()
	b := make([]byte, 2)
	rand.Read(b)
	return fmt.Sprintf("%s-%s",
		now.Format("060102-150405"),
		fmt.Sprintf("%02x%02x", b[0], b[1]),
	)
}
