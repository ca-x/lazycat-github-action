package releasenotes_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/releasenotes"
)

func TestPlainStoreNotesAstrLink(t *testing.T) {
	source, err := os.ReadFile("testdata/astrlink.md")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/astrlink.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"", releasenotes.StoreFormatPlain} {
		got, err := releasenotes.FormatStore(string(source), format)
		if err != nil || got != strings.TrimSuffix(string(expected), "\n") {
			t.Fatalf("plain notes: %v\ngot:\n%s\nwant:\n%s", err, got, expected)
		}
	}
	raw, err := releasenotes.FormatStore(string(source), releasenotes.StoreFormatMarkdown)
	if err != nil || raw != string(source) {
		t.Fatalf("Markdown source changed: %v", err)
	}
}

func TestPlainStoreNotesSemantics(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"literal punctuation", `2 * 3 = 6; snake_case; \*literal\*; \# text`, "2 * 3 = 6; snake_case; *literal*; # text"},
		{"adjacent inline formatting", "**one** *two* ~~three~~", "one two three"},
		{"soft breaks between inline elements", "**one**\n**two**", "one\ntwo"},
		{"links and references", "[Docs][guide] <https://example.com/>\n\n[guide]: https://example.com/docs?a=1&b=2", "Docs (https://example.com/docs?a=1&b=2) https://example.com/"},
		{"image alt and URL", "![Screenshot](https://example.com/image.png)", "Screenshot (https://example.com/image.png)"},
		{"HTML and entities", "<div>Hello <b>world</b> &amp; friends<br>second line</div>\n\n<script>private script</script>\n<style>private style</style>", "Hello world & friends\nsecond line"},
		{"fenced code stays literal", "```sh\necho '**literal**'\n  echo '<b>'\n```", "echo '**literal**'\n  echo '<b>'"},
		{"quoted code stays literal", "> ```text\n> [!IMPORTANT]\n> ```", "[!IMPORTANT]"},
		{"quoted inline code stays literal", "> `[!NOTE]`", "[!NOTE]"},
		{"ordered and nested lists", "3. first\n4. second\n   - nested\n   - more\n5. last", "3. first\n4. second\n  • nested\n  • more\n5. last"},
		{"task lists", "- [x] **done**\n- [ ] pending", "• ☑ done\n• ☐ pending"},
		{"table", "| Feature | Status |\n| --- | --- |\n| **Build** | Done |", "Feature | Status\nBuild | Done"},
		{"empty formatting", "---\n<!-- comment -->\n<script>hidden</script>", ""},
		{"large entity expansion", strings.Repeat("&", 20000), strings.Repeat("&", 20000)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := releasenotes.FormatStore(test.source, releasenotes.StoreFormatPlain)
			if err != nil || got != test.want {
				t.Fatalf("FormatStore()=%q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestStoreNotesConversionLimits(t *testing.T) {
	sources := []string{
		strings.Repeat("中", releasenotes.MaxChangelogBytes),
		strings.Repeat("[x][ref] ", 500) + "\n\n[ref]: https://example.com/" + strings.Repeat("a", 1000),
	}
	for _, source := range sources {
		if _, err := releasenotes.FormatStore(source, releasenotes.StoreFormatPlain); err == nil {
			t.Fatal("accepted oversized notes")
		}
	}
	if _, err := releasenotes.FormatStore("secret body", "html"); err == nil || strings.Contains(err.Error(), "secret body") {
		t.Fatalf("invalid format error=%v", err)
	}
}
