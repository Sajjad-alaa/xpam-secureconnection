package tunnel

import (
	"strconv"
	"sync"
)

// remoteAppKeyFilter drops key combinations that leave a RemoteApp program for the Windows
// shell: the Windows key (Start menu, Win+R, Win+E, ...), Ctrl+Alt+Del / Ctrl+Alt+End
// (security screen with Task Manager) and Ctrl+Shift+Esc (Task Manager). It runs in koko,
// so a modified web client cannot bypass it.
type remoteAppKeyFilter struct {
	mu      sync.Mutex
	pressed map[int]bool // modifier keysyms currently held
	blocked map[int]bool // keys whose press was dropped (drop the release as well)
}

const (
	keyShiftL, keyShiftR     = 0xffe1, 0xffe2
	keyControlL, keyControlR = 0xffe3, 0xffe4
	keyMetaL, keyMetaR       = 0xffe7, 0xffe8
	keyAltL, keyAltR         = 0xffe9, 0xffea
	keySuperL, keySuperR     = 0xffeb, 0xffec
	keyHyperL, keyHyperR     = 0xffed, 0xffee
	keyAltGr                 = 0xfe03
	keyEscape                = 0xff1b
	keyEnd, keyKPEnd         = 0xff57, 0xff9c
	keyDelete, keyKPDelete   = 0xffff, 0xff9f
)

func newRemoteAppKeyFilter() *remoteAppKeyFilter {
	return &remoteAppKeyFilter{pressed: map[int]bool{}, blocked: map[int]bool{}}
}

func (f *remoteAppKeyFilter) held(keys ...int) bool {
	for _, k := range keys {
		if f.pressed[k] {
			return true
		}
	}
	return false
}

// allow reports whether a guacd "key" instruction (args: keysym, pressed) may be forwarded.
func (f *remoteAppKeyFilter) allow(args []string) bool {
	if len(args) < 2 {
		return true
	}
	keysym, err := strconv.Atoi(args[0])
	if err != nil {
		return false
	}
	down := args[1] == "1"

	f.mu.Lock()
	defer f.mu.Unlock()

	switch keysym {
	case keySuperL, keySuperR, keyMetaL, keyMetaR, keyHyperL, keyHyperR:
		return false
	case keyShiftL, keyShiftR, keyControlL, keyControlR, keyAltL, keyAltR, keyAltGr:
		f.pressed[keysym] = down
		return true
	}

	if !down {
		if f.blocked[keysym] {
			delete(f.blocked, keysym)
			return false
		}
		return true
	}

	ctrl := f.held(keyControlL, keyControlR)
	alt := f.held(keyAltL, keyAltR, keyAltGr)
	shift := f.held(keyShiftL, keyShiftR)
	deny := false
	switch keysym {
	case keyDelete, keyKPDelete, keyEnd, keyKPEnd:
		deny = ctrl && alt
	case keyEscape:
		deny = ctrl && shift
	}
	if deny {
		f.blocked[keysym] = true
		return false
	}
	return true
}
