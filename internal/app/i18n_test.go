package app

import (
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/downmygo/downmygo/internal/model"
)

// speak makes the app show the language of tag for one test.
func speak(t *testing.T, tag string) {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return tag })
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return "en" }) })
}

func TestViewSpeaksEachLanguage(t *testing.T) {
	cases := []struct {
		tag  string
		want []string
	}{
		{"en", []string{"downmygo", "All Downloads", "Add URL", "Finished", "Details"}},
		{"zh-Hans", []string{"全部下载", "添加链接", "已完成", "详情", "设置"}},
		{"zh-Hant", []string{"全部下載", "新增連結", "已完成", "詳細資料", "設定"}},
	}
	for _, c := range cases {
		t.Run(c.tag, func(t *testing.T) {
			speak(t, c.tag)
			a := demoApp(t)
			tt := ui.NewTester(a.View, 1120, 720)
			for _, want := range c.want {
				if !tt.HasText(want) {
					t.Errorf("%s: missing %q in %q", c.tag, want, tt.Texts())
				}
			}
		})
	}
}

func TestBuiltinNamesFollowTheLanguage(t *testing.T) {
	speak(t, "zh-Hans")
	cats := model.DefaultCategories("/tmp")
	for _, c := range cats {
		if c.ID == "video" && categoryName(c) != "视频" {
			t.Errorf("video = %q", categoryName(c))
		}
		if c.ID == "general" && categoryName(c) != "常规" {
			t.Errorf("general = %q", categoryName(c))
		}
	}
	mine := model.Category{ID: "mine", Name: "My things"}
	if got := categoryName(mine); got != "My things" {
		t.Errorf("a category of the user's = %q", got)
	}
	if got := queueName(model.DefaultQueues()[0]); got != "主下载队列" {
		t.Errorf("main queue = %q", got)
	}
	if got := queueName(model.Queue{ID: "q", Name: "Night"}); got != "Night" {
		t.Errorf("a queue of the user's = %q", got)
	}
}

func TestStatusAndTimeAreTranslated(t *testing.T) {
	speak(t, "zh-Hant")
	d := &model.Download{Status: model.StatusDownloading}
	if got := statusText(d); got != "下載中" {
		t.Errorf("downloading = %q", got)
	}
	if got := resumableText(true); got != "支援續傳" {
		t.Errorf("resumable = %q", got)
	}
	if got := etaText(2*60e9 + 51e9); got != "2 分 51 秒" {
		t.Errorf("eta = %q", got)
	}
}

func TestLanguageKeysAndLabels(t *testing.T) {
	speak(t, "en")
	keys := languageKeys()
	if keys[0] != "" || len(keys) != 4 {
		t.Fatalf("language keys = %v", keys)
	}
	if got := keyAt(keys, labelIndex(keys, "zh-Hant")); got != "zh-Hant" {
		t.Errorf("round trip = %q", got)
	}
	if got := keyAt(keys, 99); got != "" {
		t.Errorf("out of range = %q", got)
	}
	labels := (&App{}).languageLabels()
	if labels[0] != "Follow the system" || labels[2] != "简体中文" || labels[3] != "繁體中文" {
		t.Errorf("labels = %v", labels)
	}
}

func TestCompactLayoutLeavesOutColumns(t *testing.T) {
	a := demoApp(t)
	wide := a.visibleColumns(1120)
	if len(wide) != 6 {
		t.Errorf("wide columns = %v", wide)
	}
	narrow := a.visibleColumns(600)
	if len(narrow) != 2 || narrow[0] != colName || narrow[1] != colProgress {
		t.Errorf("narrow columns = %v", narrow)
	}
	medium := a.visibleColumns(760)
	want := []colKind{colName, colSize, colProgress, colStatus}
	if len(medium) != len(want) {
		t.Fatalf("medium columns = %v", medium)
	}
	for i := range want {
		if medium[i] != want[i] {
			t.Fatalf("medium columns = %v", medium)
		}
	}
}
