package themes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndLoad(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "name": "Nord",
  "base": "dark",
  "colors": {"accent": "#88C0D0", "background": "#2E3440"},
  "radius": 12
}`
	if err := os.WriteFile(filepath.Join(dir, "nord.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"colors":{"nope":"#fff"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	list, errs := Load(dir)
	if len(list) != 1 || list[0].Name != "Nord" || list[0].Base != "dark" {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Colors["accent"] != "#88C0D0" || list[0].Radius != 12 {
		t.Fatalf("theme = %+v", list[0])
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
}

func TestReadRefusesBadValues(t *testing.T) {
	dir := t.TempDir()
	cases := []string{
		`{"colors":{"accent":"blue"}}`,
		`{"colors":{"notARole":"#ffffff"}}`,
		`{"base":"sepia"}`,
		`not json`,
	}
	for _, body := range cases {
		path := filepath.Join(dir, "t.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "themes")
	path, err := Write(dir, Theme{Name: "My Theme", Base: "light", Colors: map[string]string{"accent": "#123456"}})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "my-theme.json" {
		t.Fatalf("path = %s", path)
	}
	back, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name != "My Theme" || back.Colors["accent"] != "#123456" {
		t.Fatalf("back = %+v", back)
	}
}

func TestLoadMissingFolder(t *testing.T) {
	list, errs := Load(filepath.Join(t.TempDir(), "nope"))
	if len(list) != 0 || len(errs) != 0 {
		t.Fatalf("%v %v", list, errs)
	}
}
