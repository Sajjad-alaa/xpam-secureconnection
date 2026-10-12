package session

import (
	"path"
	"regexp"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/logger"
)

// XPAM native RDP RemoteApp: an asset on the Windows-RemoteApp platform publishes exactly one
// Windows program. Core validates and stores the settings in the asset info
// (remote_app / remote_app_dir / remote_app_args) and sends them in the connection token.

const RemoteAppPlatformName = "Windows-RemoteApp"

// Requested when a RemoteApp asset has no usable program: Windows cannot start it, so the user
// gets no window instead of falling back to the full desktop of the server.
const remoteAppUnavailable = "||xpam-remoteapp-not-configured"

var (
	controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)
	remoteAppAlias = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._+-]{0,63}$`)
)

// RemoteApp windows look much better with these; desktops keep the bandwidth-saving defaults.
var remoteAppDisplayParams = map[string]string{
	guacd.RDPEnableFontSmoothing:  BoolTrue,
	guacd.RDPEnableTheming:        BoolTrue,
	guacd.RDPEnableFullWindowDrag: BoolTrue,
}

type remoteAppSetting struct {
	Program string
	Dir     string
	Args    string
}

// isRemoteAppAsset uses the connection token's platform: the token's asset object does not
// carry its platform, so asset.Platform is usually empty here.
func isRemoteAppAsset(asset *model.Asset, platform *model.Platform) bool {
	if asset == nil {
		return false
	}
	if platform != nil && platform.Name != "" {
		return platform.Name == RemoteAppPlatformName
	}
	return asset.Platform.Name == RemoteAppPlatformName
}

func infoString(asset *model.Asset, key string) string {
	if asset == nil || asset.Info == nil {
		return ""
	}
	if value, ok := asset.Info[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// normalizeRemoteAppProgram applies the MS-RDPERP rule: full paths are sent without the "||"
// prefix (otherwise Windows looks the path up as an alias and rejects it), aliases with it.
func normalizeRemoteAppProgram(program string) (string, bool) {
	program = strings.TrimSpace(strings.Trim(strings.TrimSpace(program), `"`))
	if program == "" || controlChars.MatchString(program) {
		return "", false
	}
	if strings.ContainsAny(program, `\/`) {
		return strings.TrimLeft(program, "|"), true
	}
	alias := strings.TrimLeft(program, "|")
	if !remoteAppAlias.MatchString(alias) {
		return "", false
	}
	return "||" + alias, true
}

func remoteAppSettingOf(asset *model.Asset) remoteAppSetting {
	program, ok := normalizeRemoteAppProgram(infoString(asset, "remote_app"))
	if !ok {
		logger.Errorf("RemoteApp asset %s (%s) has no valid program configured", asset.Name, asset.ID)
		program = remoteAppUnavailable
	}
	setting := remoteAppSetting{Program: program}
	if dir := infoString(asset, "remote_app_dir"); dir != "" && !controlChars.MatchString(dir) {
		setting.Dir = strings.Trim(dir, `"`)
	}
	if args := infoString(asset, "remote_app_args"); args != "" && !controlChars.MatchString(args) {
		setting.Args = args
	}
	return setting
}

// applyRemoteApp switches an RDP configuration to RemoteApp mode for Windows-RemoteApp assets.
func applyRemoteApp(conf *guacd.Configuration, asset *model.Asset, platform *model.Platform) {
	if !isRemoteAppAsset(asset, platform) {
		return
	}
	setting := remoteAppSettingOf(asset)
	// NLA checks the credentials before the session starts, so a wrong password is reported
	// as an authentication error (769) instead of an invisible Windows logon screen.
	conf.SetParameter(guacd.RDPSecurity, SecurityNla)
	conf.SetParameter(guacd.RDPRemoteApp, setting.Program)
	conf.SetParameter(guacd.RDPRemoteAppDir, setting.Dir)
	conf.SetParameter(guacd.RDPRemoteAppArgs, setting.Args)
	for key, value := range remoteAppDisplayParams {
		conf.SetParameter(key, value)
	}
	logger.Infof("RemoteApp session for asset %s: program %s", asset.Name, setting.Program)
}

// remoteAppDisplayName is the program name shown in the web UI (e.g. "qgis-bin", "notepad").
func remoteAppDisplayName(asset *model.Asset) string {
	program := remoteAppSettingOf(asset).Program
	name := path.Base(strings.ReplaceAll(strings.TrimLeft(program, "|"), `\`, "/"))
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), ".EXE")
	if name == "" || program == remoteAppUnavailable {
		return asset.Name
	}
	return name
}
