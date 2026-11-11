package frontend

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
	// MyGo's own words (menus, dialogs, controls) in Simplified Chinese.
	_ "github.com/egoist/mygo/locales/zh-Hans"
)

// languages are the languages the app speaks: its own text in English, and a
// Chinese translation in zh.go. MyGo's controls and menus follow the same
// choice, and so do the pages (they hear the language from MyGo).
var languages = []string{"en", "zh-Hans"}

// shown is the language the app shows, kept here because tr is called from
// every frame and from tests, where asking MyGo (which answers on its main
// thread) would block. It is set when the app starts and when the language
// changes; until then it is English.
var shown atomic.Value

// rememberLanguage records the language MyGo shows. Call it on the main thread.
func rememberLanguage() {
	shown.Store(mygo.App.Locale())
}

// chineseShown reports whether the app shows Chinese now.
func chineseShown() bool {
	tag, _ := shown.Load().(string)
	return strings.HasPrefix(strings.ToLower(tag), "zh")
}

// tr says an English text in the language the app shows. The English is the
// key: a text that zh.go lacks shows as written. With arguments it is a
// format, as fmt's (%s, %d): the Chinese keeps the arguments' order, so write
// it with indexed verbs (%[2]s) if it must say them the other way round.
func tr(en string, args ...any) string {
	text := en
	if chineseShown() {
		if cn, ok := zh[en]; ok {
			text = cn
		}
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// languageChoices are what the Language setting offers: "" follows the
// system, then each language by its own name.
var languageChoices = []string{"", "en", "zh-Hans"}

// languageName names a choice for the Language setting.
func languageName(tag string) string {
	switch tag {
	case "en":
		return "English"
	case "zh-Hans":
		return "简体中文"
	}
	return tr("Automatic")
}

// applyLanguage shows the language the settings ask for: "" follows the
// system's languages (Chinese for a Chinese reader, else English), "en" or
// "zh-Hans" is the user's choice. MyGo draws native windows anew, rebuilds the
// roles' menu items and tells the pages, which mount themselves again.
func applyLanguage(tag string) {
	mygo.App.SetLocale(tag)
}

// dateTimeLayout is how a date and time are written in the language shown.
func dateTimeLayout() string {
	if chineseShown() {
		return "2006年1月2日 15:04"
	}
	return "2 Jan 2006, 15:04"
}

// shortTimeLayout is a short date and time, for lists.
func shortTimeLayout() string {
	if chineseShown() {
		return "1月2日 15:04"
	}
	return "Jan 2 15:04"
}
