package i18n

import (
	"path"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/jumpserver/koko/pkg/config"
)

// textDomain is the name of the translation files (locale/<lang>/LC_MESSAGES/<textDomain>.mo).
const textDomain = "secureconnection"

func Initial() {
	cf := config.GetConf()
	localePath := path.Join(cf.RootPath, "locale")
	lowerCode := strings.ToLower(cf.LanguageCode)
	gotext.Configure(localePath, lowerCode, textDomain)
	setupLangMap(localePath)
}

func setupLangMap(localePath string) {
	for _, code := range allLangCodes {
		enLocal := gotext.NewLocale(localePath, code.String())
		enLocal.AddDomain(textDomain)
		langMap[code] = enLocal
	}
}

// NewLang resolves any language code to English: XPAM is English only (a saved "zh" no longer
// switches the terminal to the built-in Chinese texts).
func NewLang(code string) LanguageCode {
	return EN
}

func T(s string) string {
	// Look up the message without interpreting its placeholders as arguments.
	return gotext.Get(s, []any{}...)
}
