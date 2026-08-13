package i18n

import (
	"os"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := map[string]Lang{
		"zh":      Zh,
		"zh_CN":   Zh,
		"zh-cn":   Zh,
		"中文":      Zh,
		"en":      En,
		"EN_US":   En,
		"English": En,
		"fr":      Auto,
		"":        Auto,
	}
	for in, want := range cases {
		if got := normalize(Lang(in)); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestT(t *testing.T) {
	defer func() { lang = En }()
	lang = En
	if got := T("hello", "你好"); got != "hello" {
		t.Errorf("en: got %q", got)
	}
	lang = Zh
	if got := T("hello", "你好"); got != "你好" {
		t.Errorf("zh: got %q", got)
	}
}

func TestIsChinaRegion(t *testing.T) {
	time.Local = time.FixedZone("CST", 8*3600)
	if !isChinaRegion() {
		t.Error("expect China region with UTC+8")
	}
	time.Local = time.FixedZone("EST", -5*3600)
	if isChinaRegion() {
		t.Error("expect non-China region with UTC-5")
	}
	time.Local = time.FixedZone("Asia/Shanghai", 8*3600)
	if !isChinaRegion() {
		t.Error("expect China region by zone name")
	}
	time.Local = nil
}

func TestIsUTF8Terminal(t *testing.T) {
	oldLANG := os.Getenv("LANG")
	defer os.Setenv("LANG", oldLANG)
	os.Setenv("LANG", "zh_CN.UTF-8")
	if !isUTF8Terminal() {
		t.Error("zh_CN.UTF-8 should be treated as UTF-8 capable")
	}
	os.Setenv("LANG", "C")
	if isUTF8Terminal() {
		t.Error("C locale should not be treated as UTF-8 capable")
	}
}

func TestResolve(t *testing.T) {
	defer func() { lang = En }()
	if got := Resolve("zh"); got != Zh {
		t.Errorf("Resolve(zh) = %q", got)
	}
	if got := Resolve("en"); got != En {
		t.Errorf("Resolve(en) = %q", got)
	}
}
