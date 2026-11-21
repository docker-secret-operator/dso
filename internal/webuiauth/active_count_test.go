package webuiauth

import (
	"testing"
	"time"
)

func TestActiveCount_ExcludesExpiredAndDoesNotRefresh(t *testing.T) {
	hash, _ := HashPassword("pw-for-active-count-test")
	m := NewManager("op", hash, time.Minute)
	m.SetMaxLifetime(time.Hour)
	now := time.Now()
	m.now = func() time.Time { return now }
	a, err := m.Login("op", "pw-for-active-count-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Login("op", "pw-for-active-count-test"); err != nil {
		t.Fatal(err)
	}
	if got := m.ActiveCount(); got != 2 {
		t.Fatalf("ActiveCount = %d, want 2", got)
	}
	now = now.Add(30 * time.Second)
	_ = m.ActiveCount()
	now = now.Add(45 * time.Second) // 75s since login; counting must not have refreshed idle timers
	if got := m.ActiveCount(); got != 0 {
		t.Fatalf("after idle expiry ActiveCount = %d, want 0", got)
	}
	_ = a
}
