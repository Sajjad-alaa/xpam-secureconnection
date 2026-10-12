package session

import (
	"path"
	"regexp"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/spf13/viper"

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
	remoteAppControlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)
	remoteAppAlias        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._+-]{0,63}$`)
	// Local absolute path (drive letter); UNC paths could load a program from another host.
	localWinPath = regexp.MustCompile(`^[A-Za-z]:\\[^/:*?"<>|]*$`)
)

// Shells, script hosts and admin tools; kept in sync with DENIED_PROGRAMS in core
// (apps/xpack/remote_app.py) so koko refuses them even if core validation were bypassed.
var deniedRemoteAppPrograms = map[string]bool{
	"cmd": true, "command": true, "powershell": true, "powershell_ise": true, "pwsh": true,
	"wsl": true, "bash": true, "conhost": true, "explorer": true, "taskmgr": true, "mmc": true,
	"regedit": true, "regedt32": true, "control": true, "msconfig": true, "servermanager": true,
	"wscript": true, "cscript": true, "mshta": true, "rundll32": true, "regsvr32": true,
	"msiexec": true, "wmic": true, "runas": true, "certutil": true, "bitsadmin": true,
	"schtasks": true, "sc": true, "net": true, "net1": true, "netsh": true, "reg": true, "at": true,
}

func isDeniedRemoteAppProgram(name string) bool {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.TrimSuffix(base, ".exe")
	return deniedRemoteAppPrograms[base]
}

// RemoteApp windows look much better with these; desktops keep the bandwidth-saving defaults.
// Applied only when the operator has not set the corresponding environment variable.
var remoteAppDisplayDefaults = map[string]string{ // env key -> guacd parameter
	"JUMPSERVER_ENABLE_FONT_SMOOTHING":   guacd.RDPEnableFontSmoothing,
	"JUMPSERVER_ENABLE_THEMING":          guacd.RDPEnableTheming,
	"JUMPSERVER_ENABLE_FULL_WINDOW_DRAG": guacd.RDPEnableFullWindowDrag,
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
	if platform != nil && platform.Name == RemoteAppPlatformName {
		return true
	}
	if asset.Platform.Name == RemoteAppPlatformName {
		return true
	}
	// Fail closed: a configured program makes the asset RemoteApp-only even if the platform
	// name does not match (core clears these keys when an asset leaves the platform).
	return infoString(asset, "remote_app") != ""
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
	if program == "" || remoteAppControlChars.MatchString(program) {
		return "", false
	}
	if strings.ContainsAny(program, `\/`) {
		program = strings.ReplaceAll(strings.TrimLeft(program, "|"), "/", `\`)
		if !localWinPath.MatchString(program) || !strings.HasSuffix(strings.ToLower(program), ".exe") {
			return "", false
		}
		if isDeniedRemoteAppProgram(program[strings.LastIndex(program, `\`)+1:]) {
			return "", false
		}
		return program, true
	}
	alias := strings.TrimLeft(program, "|")
	if !remoteAppAlias.MatchString(alias) || isDeniedRemoteAppProgram(alias) {
		return "", false
	}
	return "||" + alias, true
}

func remoteAppSettingOf(asset *model.Asset) remoteAppSetting {
	return remoteAppSettingWithLog(asset, true)
}

func remoteAppSettingWithLog(asset *model.Asset, logProblems bool) remoteAppSetting {
	program, ok := normalizeRemoteAppProgram(infoString(asset, "remote_app"))
	if !ok {
		if logProblems {
			logger.Errorf("RemoteApp asset %s (%s) has no valid program configured", asset.Name, asset.ID)
		}
		program = remoteAppUnavailable
	}
	setting := remoteAppSetting{Program: program}
	if dir := strings.Trim(infoString(asset, "remote_app_dir"), `"`); dir != "" {
		if localWinPath.MatchString(dir) {
			setting.Dir = dir
		} else {
			if logProblems {
				logger.Errorf("RemoteApp asset %s: ignoring invalid working directory", asset.Name)
			}
		}
	}
	if args := infoString(asset, "remote_app_args"); args != "" {
		if !remoteAppControlChars.MatchString(args) && !strings.Contains(args, "%") && !strings.Contains(args, `\\`) {
			setting.Args = args
		} else {
			if logProblems {
				logger.Errorf("RemoteApp asset %s: ignoring unsafe arguments", asset.Name)
			}
		}
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
	if current := conf.Parameters[guacd.RDPSecurity]; current != SecurityNla && current != SecurityNlaExt {
		if current != SecurityAny {
			logger.Infof("RemoteApp asset %s: RDP security %q replaced by nla", asset.Name, current)
		}
		conf.SetParameter(guacd.RDPSecurity, SecurityNla)
	}
	conf.SetParameter(guacd.RDPRemoteApp, setting.Program)
	conf.SetParameter(guacd.RDPRemoteAppDir, setting.Dir)
	conf.SetParameter(guacd.RDPRemoteAppArgs, setting.Args)
	for envKey, param := range remoteAppDisplayDefaults {
		if viper.GetString(envKey) == "" {
			conf.SetParameter(param, BoolTrue)
		}
	}
	logger.Infof("RemoteApp session for asset %s: program %s", asset.Name, setting.Program)
}

// remoteAppDisplayName is the program name shown in the web UI (e.g. "qgis-bin", "notepad").
func remoteAppDisplayName(asset *model.Asset) string {
	program := remoteAppSettingWithLog(asset, false).Program
	name := path.Base(strings.ReplaceAll(strings.TrimLeft(program, "|"), `\`, "/"))
	if strings.EqualFold(path.Ext(name), ".exe") {
		name = name[:len(name)-len(path.Ext(name))]
	}
	if name == "" || program == remoteAppUnavailable {
		return asset.Name
	}
	return name
}

// ForBrowser returns a copy that is safe to send to the web client: the asset info (gathered
// hardware inventory, custom info such as program paths) is only needed inside koko.
func (s TunnelSession) ForBrowser() TunnelSession {
	if s.Asset != nil && s.Asset.Info != nil {
		asset := *s.Asset
		asset.Info = nil
		s.Asset = &asset
	}
	return s
}

// IsNativeRemoteApp reports a Windows-RemoteApp session (not a JumpServer applet session).
func (s TunnelSession) IsNativeRemoteApp() bool {
	return s.AppletOpts == nil && s.VirtualAppOpts == nil && isRemoteAppAsset(s.Asset, s.Platform)
}
