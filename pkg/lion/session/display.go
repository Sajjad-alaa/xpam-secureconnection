package session

import (
	"strings"

	"github.com/spf13/viper"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

type valueType string

const (
	Boolean valueType = "boolean"
	String  valueType = "string"
	Integer valueType = "integer"
)

type DisplayParameter struct {
	Key          string
	DefaultValue string
	valueType    valueType
}

var (
	colorDepth               = DisplayParameter{Key: guacd.RDPColorDepth, DefaultValue: "24", valueType: Integer}
	dpi                      = DisplayParameter{Key: guacd.RDPDpi, DefaultValue: "", valueType: Integer}
	disableAudio             = DisplayParameter{Key: guacd.RDPDisableAudio, DefaultValue: "", valueType: Boolean}
	enableWallpaper          = DisplayParameter{Key: guacd.RDPEnableWallpaper, DefaultValue: "", valueType: Boolean}
	enableTheming            = DisplayParameter{Key: guacd.RDPEnableTheming, DefaultValue: "", valueType: Boolean}
	enableFontSmoothing      = DisplayParameter{Key: guacd.RDPEnableFontSmoothing, DefaultValue: "", valueType: Boolean}
	enableFullWindowDrag     = DisplayParameter{Key: guacd.RDPEnableFullWindowDrag, DefaultValue: "", valueType: Boolean}
	enableDesktopComposition = DisplayParameter{Key: guacd.RDPEnableDesktopComposition, DefaultValue: "", valueType: Boolean}
	enableMenuAnimations     = DisplayParameter{Key: guacd.RDPEnableMenuAnimations, DefaultValue: "", valueType: Boolean}
	disableBitmapCaching     = DisplayParameter{Key: guacd.RDPDisableBitmapCaching, DefaultValue: "", valueType: Boolean}
	disableOffscreenCaching  = DisplayParameter{Key: guacd.RDPDisableOffscreenCaching, DefaultValue: "", valueType: Boolean}
	vncCursorRender          = DisplayParameter{Key: guacd.VNCCursor, DefaultValue: "", valueType: String}
	enableConsoleAudio       = DisplayParameter{Key: guacd.RDPConsoleAudio, DefaultValue: "", valueType: Boolean}
	enableAudioInput         = DisplayParameter{Key: guacd.RDPEnableAudioInput, DefaultValue: "", valueType: Boolean}
)

type Display struct {
	data map[string]DisplayParameter
}

func (d Display) GetDisplayParams() map[string]string {
	res := make(map[string]string)
	for envKey, displayParam := range d.data {
		res[displayParam.Key] = displayParam.DefaultValue
		if value := displayEnv(envKey); value != "" {
			switch displayParam.valueType {
			case Boolean:
				booleanValue := viper.GetBool(envKey)
				res[displayParam.Key] = ConvertBoolToString(booleanValue)
			default:
				res[displayParam.Key] = value
			}
		}
	}
	return res
}

var RDPDisplay = Display{data: map[string]DisplayParameter{
	"XPAM_COLOR_DEPTH":                colorDepth,
	"XPAM_DPI":                        dpi,
	"XPAM_DISABLE_AUDIO":              disableAudio,
	"XPAM_ENABLE_WALLPAPER":           enableWallpaper,
	"XPAM_ENABLE_THEMING":             enableTheming,
	"XPAM_ENABLE_FONT_SMOOTHING":      enableFontSmoothing,
	"XPAM_ENABLE_FULL_WINDOW_DRAG":    enableFullWindowDrag,
	"XPAM_ENABLE_DESKTOP_COMPOSITION": enableDesktopComposition,
	"XPAM_ENABLE_MENU_ANIMATIONS":     enableMenuAnimations,
	"XPAM_DISABLE_BITMAP_CACHING":     disableBitmapCaching,
	"XPAM_DISABLE_OFFSCREEN_CACHING":  disableOffscreenCaching,
	"XPAM_ENABLE_CONSOLE_AUDIO":       enableConsoleAudio,
	"XPAM_ENABLE_AUDIO_INPUT":         enableAudioInput,
}}

var VNCDisplay = Display{data: map[string]DisplayParameter{
	"XPAM_COLOR_DEPTH":       colorDepth,
	"XPAM_VNC_CURSOR_RENDER": vncCursorRender,
}}

var RDPBuiltIn = map[string]string{
	guacd.RDPDisableGlyphCaching: BoolTrue,
}

// displayEnv reads a display option from the environment: the XPAM_* name, or the name it had before the rename
// (JUMPSERVER_*), which is still honoured.
func displayEnv(key string) string {
	if value := viper.GetString(key); value != "" {
		return value
	}
	if strings.HasPrefix(key, "XPAM_") {
		return viper.GetString("JUMPSERVER_" + strings.TrimPrefix(key, "XPAM_"))
	}
	return ""
}
