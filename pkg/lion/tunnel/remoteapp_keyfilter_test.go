package tunnel

import (
	"strconv"
	"testing"
)

func key(sym int, down bool) []string {
	d := "0"
	if down {
		d = "1"
	}
	return []string{strconv.Itoa(sym), d}
}

func TestRemoteAppKeyFilter(t *testing.T) {
	f := newRemoteAppKeyFilter()
	// Windows key is always dropped (press and release)
	if f.allow(key(keySuperL, true)) || f.allow(key(keySuperL, false)) {
		t.Fatal("Super key must be blocked")
	}
	// Ctrl+Alt+Del: modifiers pass, Delete press and its release are dropped
	if !f.allow(key(keyControlL, true)) || !f.allow(key(keyAltL, true)) {
		t.Fatal("modifiers must pass")
	}
	if f.allow(key(keyDelete, true)) || f.allow(key(keyDelete, false)) {
		t.Fatal("Ctrl+Alt+Del must be blocked")
	}
	if f.allow(key(keyEnd, true)) {
		t.Fatal("Ctrl+Alt+End must be blocked")
	}
	f.allow(key(keyEnd, false))
	f.allow(key(keyAltL, false))
	// Ctrl+Shift+Esc
	f.allow(key(keyShiftL, true))
	if f.allow(key(keyEscape, true)) {
		t.Fatal("Ctrl+Shift+Esc must be blocked")
	}
	f.allow(key(keyEscape, false))
	f.allow(key(keyShiftL, false))
	f.allow(key(keyControlL, false))
	// Normal typing and Alt+Tab (used by "Bring window to front") still work
	for _, k := range [][]string{key('a', true), key('a', false), key(keyDelete, true), key(keyDelete, false),
		key(keyEscape, true), key(keyEscape, false), key(keyAltL, true), key(0xff09, true), key(0xff09, false), key(keyAltL, false)} {
		if !f.allow(k) {
			t.Fatalf("key %v must pass", k)
		}
	}
	if f.allow([]string{"not-a-number", "1"}) {
		t.Fatal("malformed keysym must be dropped")
	}
}
