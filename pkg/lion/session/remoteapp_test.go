package session

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

func TestNormalizeRemoteAppProgram(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"qgis-bin", "||qgis-bin", true},
		{"||qgis-bin", "||qgis-bin", true},
		{`C:\Windows\System32\notepad.exe`, `C:\Windows\System32\notepad.exe`, true},
		{`||C:\Windows\System32\notepad.exe`, `C:\Windows\System32\notepad.exe`, true},
		{`"C:\Program Files\App\app.exe"`, `C:\Program Files\App\app.exe`, true},
		{"", "", false},
		{"bad;alias", "", false},
		{"line\nbreak", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeRemoteAppProgram(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("normalizeRemoteAppProgram(%q) = %q,%v; want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func remoteAppAsset(info map[string]interface{}) *model.Asset {
	return &model.Asset{ID: "a1", Name: "QGIS", Platform: model.BasePlatform{Name: RemoteAppPlatformName}, Info: info}
}

// Like the real connection token: the asset has no platform, the token's platform does.
func remoteAppAssetNoPlatform(info map[string]interface{}) *model.Asset {
	return &model.Asset{ID: "a1", Name: "QGIS", Info: info}
}

var remoteAppTokenPlatform = &model.Platform{Name: RemoteAppPlatformName}

func TestApplyRemoteApp(t *testing.T) {
	conf := guacd.NewConfiguration()
	conf.SetParameter(guacd.RDPSecurity, SecurityAny)
	applyRemoteApp(&conf, remoteAppAssetNoPlatform(map[string]interface{}{
		"remote_app": "qgis-bin", "remote_app_dir": `C:\QGIS`, "remote_app_args": "--noplugins",
	}), remoteAppTokenPlatform)
	want := map[string]string{
		guacd.RDPRemoteApp: "||qgis-bin", guacd.RDPRemoteAppDir: `C:\QGIS`,
		guacd.RDPRemoteAppArgs: "--noplugins", guacd.RDPSecurity: SecurityNla,
		guacd.RDPEnableFontSmoothing: BoolTrue,
	}
	for k, v := range want {
		if conf.Parameters[k] != v {
			t.Errorf("parameter %s = %q; want %q", k, conf.Parameters[k], v)
		}
	}
}

func TestRemoteAppNeverFallsBackToDesktop(t *testing.T) {
	for _, info := range []map[string]interface{}{nil, {}, {"remote_app": ""}, {"remote_app": "bad;name"}, {"remote_app": 42}} {
		conf := guacd.NewConfiguration()
		applyRemoteApp(&conf, remoteAppAssetNoPlatform(info), remoteAppTokenPlatform)
		if conf.Parameters[guacd.RDPRemoteApp] != remoteAppUnavailable {
			t.Errorf("info %v: remote-app = %q; want the unavailable placeholder", info, conf.Parameters[guacd.RDPRemoteApp])
		}
	}
}

func TestNormalWindowsAssetUntouched(t *testing.T) {
	conf := guacd.NewConfiguration()
	conf.SetParameter(guacd.RDPSecurity, SecurityAny)
	asset := &model.Asset{Platform: model.BasePlatform{Name: "Windows"}, Info: map[string]interface{}{"cpu_model": "x"}}
	applyRemoteApp(&conf, asset, &model.Platform{Name: "Windows"})
	if _, set := conf.Parameters[guacd.RDPRemoteApp]; set || conf.Parameters[guacd.RDPSecurity] != SecurityAny {
		t.Errorf("non-RemoteApp asset was modified: %v", conf.Parameters)
	}
}

func TestConfiguredProgramFailsClosed(t *testing.T) {
	// A program in the asset info keeps the asset app-only even if the platform name differs
	conf := guacd.NewConfiguration()
	asset := &model.Asset{Info: map[string]interface{}{"remote_app": "||qgis-bin"}}
	applyRemoteApp(&conf, asset, &model.Platform{Name: "Windows-RemoteApp-Copy"})
	if conf.Parameters[guacd.RDPRemoteApp] != "||qgis-bin" {
		t.Errorf("asset with a configured program must stay RemoteApp, got %v", conf.Parameters)
	}
}

func TestDeniedAndUnsafePrograms(t *testing.T) {
	for _, p := range []string{
		`C:\Windows\System32\cmd.exe`, `||powershell`, `Explorer.EXE`, `||TaskMgr`, `C:\Tools\pwsh.exe`,
		`\\evil\share\app.exe`, `C:\tools\run.bat`, `relative\app.exe`,
	} {
		if got, ok := normalizeRemoteAppProgram(p); ok {
			t.Errorf("program %q must be refused, got %q", p, got)
		}
	}
}

func TestUnsafeArgsAndDirDropped(t *testing.T) {
	s := remoteAppSettingOf(remoteAppAssetNoPlatform(map[string]interface{}{
		"remote_app": "||qgis-bin", "remote_app_args": "%COMSPEC% /c x", "remote_app_dir": `\\host\share`,
	}))
	if s.Args != "" || s.Dir != "" {
		t.Errorf("unsafe args/dir must be dropped, got args=%q dir=%q", s.Args, s.Dir)
	}
	s = remoteAppSettingOf(remoteAppAssetNoPlatform(map[string]interface{}{
		"remote_app": "||qgis-bin", "remote_app_args": `--project "C:\data\a b.qgz"`, "remote_app_dir": `C:\QGIS`,
	}))
	if s.Args == "" || s.Dir != `C:\QGIS` {
		t.Errorf("safe args/dir must be kept, got args=%q dir=%q", s.Args, s.Dir)
	}
}

func TestForBrowserOmitsAssetInfo(t *testing.T) {
	asset := remoteAppAssetNoPlatform(map[string]interface{}{"remote_app": "||x", "serial": "ABC"})
	view := TunnelSession{Asset: asset}.ForBrowser()
	if view.Asset.Info != nil {
		t.Error("browser copy must not include asset info")
	}
	if asset.Info == nil {
		t.Error("original asset info must be kept for koko")
	}
}

func TestRemoteAppDisplayName(t *testing.T) {
	if n := remoteAppDisplayName(remoteAppAsset(map[string]interface{}{"remote_app": `C:\Apps\QGIS\qgis-bin.exe`})); n != "qgis-bin" {
		t.Errorf("display name = %q; want qgis-bin", n)
	}
	if n := remoteAppDisplayName(remoteAppAsset(map[string]interface{}{"remote_app": "||notepad"})); n != "notepad" {
		t.Errorf("display name = %q; want notepad", n)
	}
}

func TestRemoteAppKeepsNlaExt(t *testing.T) {
	conf := guacd.NewConfiguration()
	conf.SetParameter(guacd.RDPSecurity, SecurityNlaExt)
	applyRemoteApp(&conf, remoteAppAssetNoPlatform(map[string]interface{}{"remote_app": "||x"}), remoteAppTokenPlatform)
	if conf.Parameters[guacd.RDPSecurity] != SecurityNlaExt {
		t.Errorf("nla-ext must be kept, got %q", conf.Parameters[guacd.RDPSecurity])
	}
}

func TestIsNativeRemoteAppExcludesAppletsAndVirtualApps(t *testing.T) {
	asset := remoteAppAssetNoPlatform(map[string]interface{}{"remote_app": "||x"})
	base := TunnelSession{Asset: asset, Platform: remoteAppTokenPlatform}
	if !base.IsNativeRemoteApp() {
		t.Error("native RemoteApp session not detected")
	}
	applet := base
	applet.AppletOpts = &model.AppletOption{}
	virtual := base
	virtual.VirtualAppOpts = &model.VirtualAppContainer{}
	if applet.IsNativeRemoteApp() || virtual.IsNativeRemoteApp() {
		t.Error("applet / virtual-app sessions must not be treated as native RemoteApp")
	}
}

func TestRemoteAppProgramBoundaries(t *testing.T) {
	long := "a"
	for len(long) < 64 {
		long += "b"
	}
	if _, ok := normalizeRemoteAppProgram(long); !ok {
		t.Error("64-char alias must be accepted")
	}
	if _, ok := normalizeRemoteAppProgram(long + "c"); ok {
		t.Error("65-char alias must be refused")
	}
	for _, p := range []string{"||", "|", "   "} {
		if _, ok := normalizeRemoteAppProgram(p); ok {
			t.Errorf("program %q must be refused", p)
		}
	}
}
