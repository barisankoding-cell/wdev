package monitor

import (
	"fmt"
	"testing"
)

func TestParseSystemdIsActiveOutput(t *testing.T) {
	cases := []struct {
		name         string
		out          string
		err          error
		wantActive   bool
		wantStarting bool
	}{
		{name: "active", out: "active\n", err: nil, wantActive: true, wantStarting: false},
		{name: "activating", out: "activating\n", err: fmt.Errorf("exit status 3"), wantActive: false, wantStarting: true},
		{name: "reloading", out: "reloading\n", err: fmt.Errorf("exit status 3"), wantActive: false, wantStarting: true},
		{name: "inactive", out: "inactive\n", err: fmt.Errorf("exit status 3"), wantActive: false, wantStarting: false},
		{name: "failed", out: "failed\n", err: fmt.Errorf("exit status 3"), wantActive: false, wantStarting: false},
		{name: "empty no err", out: "", err: nil, wantActive: true, wantStarting: false},
		{name: "empty with err", out: "", err: fmt.Errorf("exit status 3"), wantActive: false, wantStarting: false},
	}
	for _, c := range cases {
		active, starting := parseSystemdIsActiveOutput(c.out, c.err)
		if active != c.wantActive || starting != c.wantStarting {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.name, active, starting, c.wantActive, c.wantStarting)
		}
	}
}

func TestPickAnySystemdServiceStateAndStatusText(t *testing.T) {
	a, s := pickAnySystemdServiceState(func(u string) (bool, bool) {
		return false, u == "postgresql-16"
	}, "postgresql", "postgresql-16")
	if a || !s {
		t.Fatalf("got (%v, %v), want (false, true)", a, s)
	}

	a, s = pickAnySystemdServiceState(func(u string) (bool, bool) {
		if u == "redis-server" {
			return true, false
		}
		return false, true
	}, "redis-server", "redis")
	if !a || s {
		t.Fatalf("got (%v, %v), want (true, false)", a, s)
	}

	if got := serviceStatusText(true, false); got != "Running" {
		t.Fatalf("serviceStatusText(true,false) = %q", got)
	}
	if got := serviceStatusText(false, true); got != "Starting" {
		t.Fatalf("serviceStatusText(false,true) = %q", got)
	}
	if got := serviceStatusText(false, false); got != "Stopped" {
		t.Fatalf("serviceStatusText(false,false) = %q", got)
	}
}
