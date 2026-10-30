package executor

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"
	"unicode/utf8"
)

// DangerousEnvVars are environment variable keys that must never be set by user tasks.
// Setting these could lead to privilege escalation, library injection, or security bypass.
var dangerousEnvVars = map[string]bool{
	"LD_PRELOAD":               true,
	"LD_LIBRARY_PATH":          true,
	"LD_AUDIT":                 true,
	"LD_DEBUG":                 true,
	"LD_ORIGIN":                true,
	"LD_HWCAP_MASK":            true,
	"LDSO_PRELOAD":             true,
	"BASH_ENV":                 true,
	"PROMPT_COMMAND":           true,
	"PS4":                      true,
	"BASHOPTS":                 true,
	"BASH_XTRACEFD":            true,
	"ENV":                      true,
	"CDPATH":                   true,
	"GLOBIGNORE":               true,
	"IFS":                      true,
	"FUNCNEST":                 true,
	"SHELLOPTS":                true,
	"NODE_OPTIONS":             true,
	"PYTHONPATH":               true,
	"PYTHONSTARTUP":            true,
	"PERL5OPT":                 true,
	"PERL5LIB":                 true,
	"RUBYOPT":                  true,
	"GEM_PATH":                 true,
	"JAVA_TOOL_OPTIONS":        true,
	"_JAVA_OPTIONS":            true,
	"CLASSPATH":                true,
	"MANPATH":                  true,
	"INFOPATH":                 true,
	"XDG_CONFIG_HOME":          true,
	"XDG_DATA_HOME":            true,
	"HOSTALIASES":              true,
	"LOCALDOMAIN":              true,
	"RES_OPTIONS":              true,
	"EDITOR":                   true,
	"VISUAL":                   true,
	"DISPLAY":                  true,
	"WAYLAND_DISPLAY":          true,
	"DBUS_SESSION_BUS_ADDRESS": true,
	"XAUTHORITY":               true,
	"SSH_AUTH_SOCK":            true,
	"SSH_AGENT_PID":            true,
	"GPG_AGENT_INFO":           true,
	"KRB5_CONFIG":              true,
	"KRB5CCNAME":               true,
	"SYSTEMD_EDITOR":           true,
	"PAGER":                    true,
	"LESSOPEN":                 true,
	"LESSCLOSE":                true,
	"HTTP_PROXY":               true,
	"HTTPS_PROXY":              true,
	"ALL_PROXY":                true,
	"NO_PROXY":                 true,
	"http_proxy":               true,
	"https_proxy":              true,
	"all_proxy":                true,
	"no_proxy":                 true,
}

// ValidatePathValue checks a PATH value for injection. It must be a colon
// separated list of absolute directories containing no shell metacharacters
// or newlines, so it is safe to place in the task's environment.
func ValidatePathValue(v string) bool {
	if v == "" || len(v) > 4096 {
		return false
	}
	if strings.ContainsAny(v, "\n\r\x00$`\"'\\|&;<>()*?[]{}!#~ ") {
		return false
	}
	for _, dir := range strings.Split(v, ":") {
		if dir == "" || !strings.HasPrefix(dir, "/") {
			return false
		}
		if strings.Contains(dir, "..") {
			return false
		}
	}
	return true
}

// SanitizeEnvVars filters dangerous environment variables.
// Returns the safe subset and a list of rejected keys.
func SanitizeEnvVars(envVars map[string]string) (safe map[string]string, rejected []string) {
	safe = make(map[string]string, len(envVars))
	for k, v := range envVars {
		upper := strings.ToUpper(k)
		// PATH is allowed but validated: a task that needs extra tools in PATH
		// must not silently lose it, which made scheduled runs behave
		// differently from manual ones.
		if upper == "PATH" {
			if !ValidatePathValue(v) {
				rejected = append(rejected, k+" (invalid PATH)")
				continue
			}
			safe[k] = v
			continue
		}
		if dangerousEnvVars[upper] {
			rejected = append(rejected, k)
			continue
		}
		if strings.HasPrefix(upper, "LD_") {
			rejected = append(rejected, k)
			continue
		}
		// Credentials are deliberately allowed: a task that needs a database
		// password has to put it *somewhere*, and blocking DB_PASSWORD only
		// pushed users into writing it into the command line, which is worse.
		// The dangerous entries above (loader hooks, PATH, interpreters'
		// startup files) are the ones that can escalate privileges.
		// Exported shell functions (BASH_FUNC_name%%) are code, not data: a
		// task that calls `name` would execute whatever the variable carries.
		if strings.HasPrefix(upper, "BASH_FUNC_") || strings.Contains(k, "%") {
			rejected = append(rejected, k)
			continue
		}
		// Everything else is passed through. The old code kept a small
		// allow-list and silently dropped DATABASE_URL, AWS_REGION, HTTP_* and
		// friends, so tasks failed with no visible reason.
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
	if base64.StdEncoding.EncodeToString(decoded) != cmd {
		return cmd
	}
	// The roundtrip alone is not enough: plenty of ordinary commands are also
	// valid base64 ("date" -> "u\xab^", "true" -> "\xb6\xbb\x9e"). Decoding
	// those turned a plain command into garbage that failed with "command not
	// found". Only accept a decoding that is still text.
	if !looksLikeText(string(decoded)) {
		return cmd
	}
	return string(decoded)
}

// looksLikeText reports whether s is valid UTF-8 without control characters
// other than tab/newline/carriage return.
func looksLikeText(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// BuildEnv merges the process environment with a task's variables, dropping
// the ones that could be used to hijack the process. The rejected keys are
// returned so the caller can surface them in the run log (silently dropping
// DATABASE_URL made tasks fail with no visible reason).
func BuildEnv(envVars map[string]string) (env []string, rejected []string) {
	safe, rejected := SanitizeEnvVars(envVars)
	env = os.Environ()
	for k, v := range safe {
		env = append(env, k+"="+v)
	}
	return env, rejected
}

// ExpandPathForUser expands a leading ~ to the home directory of the user the
// task runs as. Using the daemon's own HOME expanded ~ to /root even when the
// task ran as an unprivileged user, which made those tasks fail immediately.
func ExpandPathForUser(path, runUser string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	if runUser != "" && runUser != "root" {
		// Only the target user's own home is correct here. Falling back to the
		// daemon's HOME is what turned ~ into /root for unprivileged users.
		u, err := user.Lookup(runUser)
		if err != nil || u.HomeDir == "" {
			return path
		}
		return u.HomeDir + strings.TrimPrefix(path, "~")
	}
	return "/root" + strings.TrimPrefix(path, "~")
}

// NewRunID generates a short human-readable run identifier.
// Format: YYMMDD-HHMMSS-xxxx (4 random hex chars) — used as fallback only.
func NewRunID() string {
	now := time.Now()
	b := make([]byte, 2)
	rand.Read(b)
	return fmt.Sprintf("%s-%s",
		now.Format("060102-150405"),
		fmt.Sprintf("%02x%02x", b[0], b[1]),
	)
}

// NewSeqRunID generates a sequential run identifier.
// Format: #taskID-user-NNNN (e.g. #1-root-0001)
func NewSeqRunID(taskID int, user string, seq int) string {
	return fmt.Sprintf("#%d-%s-%04d", taskID, user, seq)
}

// NewManualRunID generates a manual run identifier.
// Format: #taskID-user-manual-xxxx (4 random hex chars)
func NewManualRunID(taskID int, user string) string {
	b := make([]byte, 2)
	rand.Read(b)
	return fmt.Sprintf("#%d-%s-manual-%02x%02x", taskID, user, b[0], b[1])
}
