package i18n

import (
	"path"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/jumpserver/koko/pkg/config"
)

func Initial() {
	cf := config.GetConf()
	localePath := path.Join(cf.RootPath, "locale")
	lowerCode := strings.ToLower(cf.LanguageCode)
	gotext.Configure(localePath, lowerCode, "koko")
	setupLangMap(localePath)
}

func setupLangMap(localePath string) {
	for _, code := range allLangCodes {
		enLocal := gotext.NewLocale(localePath, code.String())
		enLocal.AddDomain("koko")
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
