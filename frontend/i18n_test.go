package frontend

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// literals are the English texts the sources pass to tr, and the label
// constants that are passed to it by name.
func literals(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	call := regexp.MustCompile(`\btr\("((?:[^"\\]|\\.)*)"`)
	label := regexp.MustCompile(`(?m)^\s+label\w+\s+= "((?:[^"\\]|\\.)*)"`)
	found := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "zh.go" || f == "i18n.go" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, re := range []*regexp.Regexp{call, label} {
			for _, m := range re.FindAllStringSubmatch(string(data), -1) {
				found[strings.ReplaceAll(m[1], `\"`, `"`)] = f
			}
		}
	}
	return found
}

func TestChineseCoversEveryText(t *testing.T) {
	var missing []string
	for text, file := range literals(t) {
		if _, ok := zh[text]; !ok {
			missing = append(missing, text+"  ("+file+")")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("no Chinese for:\n%s", strings.Join(missing, "\n"))
	}
}

// A format's verbs must be the same in both languages, in the same order, or
// Sprintf prints the wrong thing.
func TestChineseKeepsTheVerbs(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0]*\d*(?:\.\d+)?[sdvq]`)
	for en, cn := range zh {
		if strings.Join(verbs.FindAllString(en, -1), ",") != strings.Join(verbs.FindAllString(cn, -1), ",") {
			t.Errorf("verbs differ: %q vs %q", en, cn)
		}
	}
}

func TestTrFallsBackToEnglish(t *testing.T) {
	if got := tr("Some text nobody translated"); got != "Some text nobody translated" {
		t.Fatalf("tr = %q", got)
	}
	if got := tr("Synced %s", "Work"); got != "Synced Work" {
		t.Fatalf("tr = %q", got)
	}
}
