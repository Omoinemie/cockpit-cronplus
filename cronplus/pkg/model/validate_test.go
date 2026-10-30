package model

import (
	"strings"
	"testing"
)

func TestValidateScheduleAcceptsCommonForms(t *testing.T) {
	valid := []string{
		"* * * * *",
		"*/5 * * * *",
		"0 3 * * *",
		"0 0 1 * *",
		"30 2 * * 0",
		"0 */5 9-17 * * *",
		"0 0,12 * * 1-5",
		"@reboot", "@daily", "@hourly", "@weekly", "@monthly", "@yearly",
	}
	for _, s := range valid {
		if err := ValidateSchedule(s); err != nil {
			t.Errorf("ValidateSchedule(%q) = %v, want nil", s, err)
		}
	}
}

// `*/0` used to make the field empty, which the parser treated as "*": the task
// then ran every single second.
func TestValidateScheduleRejectsZeroStep(t *testing.T) {
	if err := ValidateSchedule("*/0 * * * *"); err == nil {
		t.Fatal("*/0 accepted")
	}
	if err := ValidateSchedule("* * * * */0"); err == nil {
		t.Fatal("*/0 in weekday accepted")
	}
}

func TestValidateScheduleRejectsOutOfRangeAndJunk(t *testing.T) {
	invalid := []string{
		"0 0 99 * * *",
		"60 * * * *",
		"* 24 * * *",
		"0 0 0 32 * *",
		"0 0 0 * 13 *",
		"0 0 0 * * 8",
		"? * * * *",
		"* * * *",
		"* * * * * * *",
		"@every 5m",
		"0 0 * * abc",
		"10-5 * * * *",
		"*/999 * * * *",
	}
	for _, s := range invalid {
		if err := ValidateSchedule(s); err == nil {
			t.Errorf("ValidateSchedule(%q) = nil, want an error", s)
		}
	}
}

func TestValidateTaskRequiresCommandAndSchedule(t *testing.T) {
	task := &Task{Command: "echo hi", Schedule: "* * * * *"}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	task.Schedule = ""
	if err := ValidateTask(task); err == nil || !strings.Contains(err.Error(), "schedule") {
		t.Fatalf("empty schedule accepted: %v", err)
	}
	task.Schedule = "* * * * *"
	task.Command = "   "
	if err := ValidateTask(task); err == nil {
		t.Fatal("blank command accepted")
	}
}

func TestValidateRunUser(t *testing.T) {
	if err := ValidateRunUser("backup"); err != nil {
		t.Fatalf("valid user rejected: %v", err)
	}
	for _, bad := range []string{"ro;ot", "root$(id)", "a b", "-x", "0day"} {
		if err := ValidateRunUser(bad); err == nil {
			t.Errorf("ValidateRunUser(%q) = nil, want an error", bad)
		}
	}
}

// Start/step ("5/10") is valid Vixie cron syntax and must keep working: older
// configs use it, and rejecting it at load time would silently stop them.
func TestValidateScheduleAcceptsStartStep(t *testing.T) {
	for _, spec := range []string{"5/10 * * * * *", "0 0/15 * * * *", "0 0 0/6 * * *"} {
		if err := ValidateSchedule(spec); err != nil {
			t.Errorf("ValidateSchedule(%q) = %v, want nil", spec, err)
		}
	}
	// Out-of-range start values are still refused.
	for _, spec := range []string{"60/5 * * * * *", "5/0 * * * * *", "*/0 * * * * *"} {
		if err := ValidateSchedule(spec); err == nil {
			t.Errorf("ValidateSchedule(%q) = nil, want an error", spec)
		}
	}
}
