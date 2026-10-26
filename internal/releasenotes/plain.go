package releasenotes

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

const (
	StoreFormatPlain    = "plain"
	StoreFormatMarkdown = "markdown"
)

func ValidateStoreFormat(format string) error {
	switch format {
	case "", StoreFormatPlain, StoreFormatMarkdown:
		return nil
	default:
		return errors.New("must be plain or markdown")
	}
}

// FormatStore runs only at the store boundary. GitHub Release bodies and Action
// changelog outputs retain the resolved source, so reconciliation never reparses
// already flattened store text as the upstream Markdown.
func FormatStore(source, format string) (string, error) {
	if err := ValidateStoreFormat(format); err != nil {
		return "", err
	}
	if len(source) > MaxChangelogBytes {
		return "", errors.New("store changelog exceeds 64 KiB")
	}
	if format == StoreFormatMarkdown {
		return source, nil
	}
	// HTML is an intermediate token stream only, never a store payload. Allow raw
	// HTML here to retain its visible text; the tokenizer removes its markup and
	// omits script/style contents below. Bound reference-link expansion as well.
	var rendered boundedOutput
	rendered.limit = 1 << 20
	markdown := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(goldhtml.WithUnsafe()))
	if err := markdown.Convert([]byte(source), &rendered); err != nil {
		return "", errors.New("store changelog conversion exceeds output limit")
	}
	plain := plainWriter{}
	tokens := html.NewTokenizer(strings.NewReader(rendered.builder.String()))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if !errors.Is(tokens.Err(), io.EOF) {
				return "", errors.New("cannot read rendered store changelog")
			}
			if plain.err != nil {
				return "", errors.New("plain store changelog exceeds 64 KiB")
			}
			return strings.Trim(plain.output.builder.String(), "\n"), nil
		case html.TextToken:
			if plain.hidden == "" {
				plain.text(string(tokens.Text()))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			plain.start(tokens.Token())
		case html.EndTagToken:
			plain.end(tokens.Token().Data)
		}
		if plain.err != nil {
			return "", errors.New("plain store changelog exceeds 64 KiB")
		}
	}
}

type plainList struct {
	ordered bool
	next    int
}

type plainWriter struct {
	output    boundedOutput
	err       error
	pending   int
	pre       bool
	inline    bool
	alert     bool
	hidden    string
	lists     []plainList
	itemStart bool
	cells     int
	linkURL   string
	linkStart int
}

func (plain *plainWriter) write(value string) {
	if value == "" || plain.err != nil {
		return
	}
	if plain.output.builder.Len() > 0 && plain.pending > 0 {
		current := plain.output.builder.String()
		trailing := len(current) - len(strings.TrimRight(current, "\n"))
		_, plain.err = plain.output.Write([]byte(strings.Repeat("\n", max(0, plain.pending-trailing))))
	}
	plain.pending = 0
	if plain.err == nil {
		_, plain.err = plain.output.Write([]byte(value))
	}
}

func (plain *plainWriter) text(value string) {
	if !plain.pre && strings.TrimSpace(value) == "" && (!plain.inline || plain.itemStart || plain.pending > 0) {
		return // Ignore whitespace inserted between the renderer's block tags.
	}
	if plain.alert {
		plain.alert = false
		first, rest, found := strings.Cut(value, "\n")
		switch strings.TrimSpace(first) {
		case "[!NOTE]", "[!TIP]", "[!IMPORTANT]", "[!WARNING]", "[!CAUTION]":
			value = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(first), "[!"), "]") + ":"
			if found {
				value += "\n" + rest
			}
		}
	}
	plain.write(value)
	plain.itemStart = false
}

func (plain *plainWriter) start(token html.Token) {
	if plain.hidden != "" {
		return
	}
	switch token.Data {
	case "script", "style":
		plain.hidden = token.Data
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "div", "table", "hr":
		if !plain.itemStart {
			plain.pending = max(plain.pending, 2)
		}
		plain.inline = token.Data != "table" && token.Data != "hr"
	case "blockquote":
		plain.pending = max(plain.pending, 2)
		plain.alert = true
	case "code":
		plain.alert = false
	case "pre":
		plain.alert = false
		plain.pending = max(plain.pending, 2)
		plain.pre = true
	case "br":
		plain.pending = max(plain.pending, 1)
	case "ul", "ol":
		if len(plain.lists) > 0 {
			plain.pending = 1 // A nested list continues its parent's item.
		} else {
			plain.pending = max(plain.pending, 1)
		}
		number, err := strconv.Atoi(attribute(token, "start"))
		if err != nil || number < 0 {
			number = 1
		}
		plain.lists = append(plain.lists, plainList{ordered: token.Data == "ol", next: number})
	case "li":
		plain.pending = max(plain.pending, 1)
		prefix := "• "
		if len(plain.lists) > 0 {
			list := &plain.lists[len(plain.lists)-1]
			if list.ordered {
				prefix = strconv.Itoa(list.next) + ". "
				list.next++
			}
		}
		plain.write(strings.Repeat("  ", max(0, len(plain.lists)-1)) + prefix)
		plain.itemStart = true
		plain.inline = true
	case "tr":
		plain.pending = max(plain.pending, 1)
		plain.cells = 0
	case "td", "th":
		if plain.cells > 0 {
			plain.write(" | ")
		}
		plain.cells++
		plain.inline = true
	case "a":
		plain.linkURL = attribute(token, "href")
		plain.linkStart = plain.output.builder.Len()
	case "img":
		alt, target := attribute(token, "alt"), attribute(token, "src")
		plain.write(alt)
		plain.destination(target, alt)
	case "input":
		if attribute(token, "type") == "checkbox" {
			mark := "☐"
			for _, attr := range token.Attr {
				if attr.Key == "checked" {
					mark = "☑"
				}
			}
			plain.write(mark)
			plain.itemStart = false
		}
	}
}

func (plain *plainWriter) end(tag string) {
	if plain.hidden != "" {
		if tag == plain.hidden {
			plain.hidden = ""
		}
		return
	}
	switch tag {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "div", "table", "pre":
		plain.pending = max(plain.pending, 2)
		plain.inline = false
		if tag == "pre" {
			plain.pre = false
		}
	case "ul", "ol":
		if len(plain.lists) > 0 {
			plain.lists = plain.lists[:len(plain.lists)-1]
		}
		plain.pending = max(plain.pending, 1)
	case "li", "tr":
		plain.pending = max(plain.pending, 1)
		plain.inline = false
	case "td", "th":
		plain.inline = false
	case "a":
		label := strings.TrimSpace(plain.output.builder.String()[plain.linkStart:])
		plain.destination(plain.linkURL, label)
		plain.linkURL = ""
	}
}

func (plain *plainWriter) destination(target, label string) {
	if target == "" || target == label || target == "mailto:"+label {
		return
	}
	if label == "" {
		plain.write(target)
	} else {
		plain.write(" (" + target + ")")
	}
}

func attribute(token html.Token, key string) string {
	for _, attr := range token.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}
