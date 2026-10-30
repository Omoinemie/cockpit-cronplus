package executor

import (
	"strings"
	"testing"
)

// Ordinary commands must survive untouched. Plain words like "date" are valid
// base64, and decoding them produced garbage such as "u\xab^".
func TestDecodeCommandLeavesPlainCommandsAlone(t *testing.T) {
	plain := []string{
		"date", "true", "echo", "test", "abcd", "id -u", "ls -l",
		"echo hi", "printf 'x'", "/usr/bin/backup.sh --daily",
	}
	for _, cmd := range plain {
		if got := DecodeCommand(cmd); got != cmd {
			t.Errorf("DecodeCommand(%q) = %q, want it unchanged", cmd, got)
		}
	}
}

func TestDecodeCommandStillDecodesBase64(t *testing.T) {
	// b64("echo hi\n") — what the WebUI writes.
	const encoded = "ZWNobyBoaQo="
	if got := DecodeCommand(encoded); got != "echo hi\n" {
		t.Fatalf("DecodeCommand(%q) = %q", encoded, got)
	}
	// Decoding must not produce control characters.
	if got := DecodeCommand("AAAA"); got != "AAAA" {
		t.Fatalf("control characters were accepted: %q", got)
	}
}

func TestSanitizeEnvVars(t *testing.T) {
	safe, rejected := SanitizeEnvVars(map[string]string{
		"DATABASE_URL":  "postgres://localhost/db",
		"DB_PASSWORD":   "secret",
		"LD_PRELOAD":    "/tmp/evil.so",
		"BASH_ENV":      "/tmp/evil.sh",
		"BASH_FUNC_x%%": "() { echo pwned; }",
		"PATH":          "/usr/local/bin:/usr/bin:/bin",
	})

	if safe["DATABASE_URL"] == "" || safe["DB_PASSWORD"] == "" {
		t.Fatalf("credentials must be passed through, got %v", safe)
	}
	if safe["PATH"] == "" {
		t.Fatal("valid PATH dropped")
	}
	joined := strings.Join(rejected, ",")
	for _, want := range []string{"LD_PRELOAD", "BASH_ENV", "BASH_FUNC_x%%"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s was not rejected: %v", want, rejected)
		}
	}
}

func TestSanitizeEnvVarsRejectsBadPath(t *testing.T) {
	if _, rejected := SanitizeEnvVars(map[string]string{"PATH": "/usr/bin:/tmp/../etc"}); len(rejected) != 1 {
		t.Fatalf("expected the PATH to be rejected, got %v", rejected)
	}
}

func TestBuildEnvIncludesTaskVars(t *testing.T) {
	env, rejected := BuildEnv(map[string]string{"APP_ENV": "prod"})
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejections: %v", rejected)
	}
	found := false
	for _, kv := range env {
		if kv == "APP_ENV=prod" {
			found = true
		}
	}
	if !found {
		t.Fatal("task env var missing from the environment")
	}
}

func TestExpandPathForUser(t *testing.T) {
	if got := ExpandPathForUser("~", "root"); got != "/root" {
		t.Fatalf("~ for root = %q", got)
	}
	if got := ExpandPathForUser("~/backups", "root"); got != "/root/backups" {
		t.Fatalf("~/backups for root = %q", got)
	}
	// Unknown users keep the path unchanged rather than silently using /root.
	if got := ExpandPathForUser("~/data", "no-such-user-xyz"); got != "~/data" {
		t.Fatalf("unknown user path = %q", got)
	}
	if got := ExpandPathForUser("/srv/app", "root"); got != "/srv/app" {
		t.Fatalf("absolute path changed: %q", got)
	}
}
