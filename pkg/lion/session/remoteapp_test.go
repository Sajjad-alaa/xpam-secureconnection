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

func TestApplyRemoteApp(t *testing.T) {
	conf := guacd.NewConfiguration()
	conf.SetParameter(guacd.RDPSecurity, SecurityAny)
	applyRemoteApp(&conf, remoteAppAsset(map[string]interface{}{
		"remote_app": "qgis-bin", "remote_app_dir": `C:\QGIS`, "remote_app_args": "--noplugins",
	}))
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
		applyRemoteApp(&conf, remoteAppAsset(info))
		if conf.Parameters[guacd.RDPRemoteApp] != remoteAppUnavailable {
			t.Errorf("info %v: remote-app = %q; want the unavailable placeholder", info, conf.Parameters[guacd.RDPRemoteApp])
		}
	}
}

func TestNormalWindowsAssetUntouched(t *testing.T) {
	conf := guacd.NewConfiguration()
	conf.SetParameter(guacd.RDPSecurity, SecurityAny)
	asset := &model.Asset{Platform: model.BasePlatform{Name: "Windows"}, Info: map[string]interface{}{"remote_app": "||x"}}
	applyRemoteApp(&conf, asset)
	if _, set := conf.Parameters[guacd.RDPRemoteApp]; set || conf.Parameters[guacd.RDPSecurity] != SecurityAny {
		t.Errorf("non-RemoteApp asset was modified: %v", conf.Parameters)
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
