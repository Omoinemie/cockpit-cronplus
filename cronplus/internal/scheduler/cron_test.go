package scheduler

import (
	"fmt"
	"testing"
	"time"
)

// Helper: compute next N run times starting from 'start'
func nextN(fields [6]string, start time.Time, n int) []time.Time {
	var results []time.Time
	current := start
	for i := 0; i < n; i++ {
		nxt := NextRunTime(fields, current)
		if nxt.IsZero() {
			break
		}
		results = append(results, nxt)
		current = nxt
	}
	return results
}

func TestNextRunTimes(t *testing.T) {
	// Use a fixed reference time: 2026-04-24 17:55:00 CST (Friday)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	ref := time.Date(2026, 4, 24, 17, 55, 0, 0, loc)

	tests := []struct {
		name     string
		schedule string
		fields   [6]string
	}{
		{
			name:     "每日 midnight (0 0 0 * * *)",
			schedule: "0 0 0 * * *",
			fields:   [6]string{"0", "0", "0", "*", "*", "*"},
		},
		{
			name:     "每周日 DOW=0 (0 0 0 * * 0)",
			schedule: "0 0 0 * * 0",
			fields:   [6]string{"0", "0", "0", "*", "*", "0"},
		},
		{
			name:     "每周日 DOW=7 (0 0 0 * * 7)",
			schedule: "0 0 0 * * 7",
			fields:   [6]string{"0", "0", "0", "*", "*", "7"},
		},
		{
			name:     "每月1号 (0 0 0 1 * *)",
			schedule: "0 0 0 1 * *",
			fields:   [6]string{"0", "0", "0", "1", "*", "*"},
		},
		{
			name:     "每隔5分钟 (*/5 * * * *)",
			schedule: "*/5 * * * *",
			fields:   [6]string{"0", "*/5", "*", "*", "*", "*"},
		},
		{
			name:     "每隔2小时 (0 */2 * * *)",
			schedule: "0 */2 * * *",
			fields:   [6]string{"0", "0", "*/2", "*", "*", "*"},
		},
		{
			name:     "@weekly",
			schedule: "@weekly",
			fields:   [6]string{"0", "0", "0", "*", "*", "0"},
		},
		{
			name:     "@daily",
			schedule: "@daily",
			fields:   [6]string{"0", "0", "0", "*", "*", "*"},
		},
		{
			name:     "@monthly",
			schedule: "@monthly",
			fields:   [6]string{"0", "0", "0", "1", "*", "*"},
		},
		{
			name:     "每周一三五 (0 0 * * 1,3,5)",
			schedule: "0 0 * * 1,3,5",
			fields:   [6]string{"0", "0", "*", "*", "*", "1,3,5"},
		},
		{
			name:     "每10秒 (*/10 * * * * *)",
			schedule: "*/10 * * * * *",
			fields:   [6]string{"*/10", "*", "*", "*", "*", "*"},
		},
		{
			name:     "每天18:30 (0 30 18 * * *)",
			schedule: "0 30 18 * * *",
			fields:   [6]string{"0", "30", "18", "*", "*", "*"},
		},
	}

	fmt.Printf("=== 参考时间: %s (周五) ===\n\n", ref.Format("2006-01-02 15:04:05 Mon"))

	for _, tt := range tests {
		fmt.Printf("--- %s [%s] ---\n", tt.name, tt.schedule)
		runs := nextN(tt.fields, ref, 5)
		for i, r := range runs {
			fmt.Printf("  第 %d 次: %s (%s)\n", i+1, r.Format("2006-01-02 15:04:05 Mon"), r.Sub(ref).Round(time.Second))
		}
		if len(runs) == 0 {
			fmt.Println("  ❌ 无匹配！调度可能有 bug")
		}
		fmt.Println()
	}
}

// Test specific DOW bug fix
func TestDOWBugFix(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")

	// Test: DOW=0 should match on Sunday
	// Sunday 2026-04-26
	sunday := time.Date(2026, 4, 26, 0, 0, 0, 0, loc)
	fields0 := [6]string{"0", "0", "0", "*", "*", "0"}
	nxt0 := NextRunTime(fields0, sunday.Add(-time.Second))
	if nxt0.Day() != 26 || nxt0.Weekday() != time.Sunday {
		t.Errorf("DOW=0: expected Sunday 04-26, got %s", nxt0)
	} else {
		fmt.Printf("✅ DOW=0 匹配周日: %s\n", nxt0.Format("2006-01-02 15:04:05 Mon"))
	}

	// Test: DOW=7 should also match on Sunday
	fields7 := [6]string{"0", "0", "0", "*", "*", "7"}
	nxt7 := NextRunTime(fields7, sunday.Add(-time.Second))
	if nxt7.Day() != 26 || nxt7.Weekday() != time.Sunday {
		t.Errorf("DOW=7: expected Sunday 04-26, got %s", nxt7)
	} else {
		fmt.Printf("✅ DOW=7 匹配周日: %s\n", nxt7.Format("2006-01-02 15:04:05 Mon"))
	}

	// Test: DOW=1 should NOT match on Sunday
	fields1 := [6]string{"0", "0", "0", "*", "*", "1"}
	nxt1 := NextRunTime(fields1, sunday.Add(-time.Second))
	if nxt1.Weekday() == time.Sunday {
		t.Errorf("DOW=1: should not match Sunday, got %s", nxt1)
	} else {
		fmt.Printf("✅ DOW=1 不匹配周日 (正确跳到 %s)\n", nxt1.Format("2006-01-02 15:04:05 Mon"))
	}

	// Test: @weekly (DOW=0) should fire on Sunday
	nxtWeekly := NextRunTime([6]string{"0", "0", "0", "*", "*", "0"}, sunday.Add(-time.Second))
	if nxtWeekly.Weekday() != time.Sunday {
		t.Errorf("@weekly: expected Sunday, got %s", nxtWeekly)
	} else {
		fmt.Printf("✅ @weekly 匹配周日: %s\n", nxtWeekly.Format("2006-01-02 15:04:05 Mon"))
	}

	fmt.Println()
}
