package tools

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

var acceptedHTMLMimeTypePrefixes = []string{
	"text/html",
	"application/xhtml",
}

var acceptedHTMLFileExtensions = map[string]struct{}{
	".html": {},
	".htm":  {},
}

type StreamInfo struct {
	MIMEType  string
	Extension string
	Filename  string
	Charset   string
	URL       string
}

type DocumentConverterResult struct {
	Markdown string
	Title    string
}

type HTMLConverter struct {
	markdown *htmltomarkdown.Converter
}

func NewHTMLConverter() *HTMLConverter {
	converter := htmltomarkdown.NewConverter("", true, nil)
	converter.Use(plugin.GitHubFlavored())
	return &HTMLConverter{markdown: converter}
}

func (c *HTMLConverter) Accepts(info StreamInfo) bool {
	extension := strings.ToLower(strings.TrimSpace(info.Extension))
	if extension == "" && info.Filename != "" {
		extension = strings.ToLower(filepath.Ext(info.Filename))
	}
	if _, ok := acceptedHTMLFileExtensions[extension]; ok {
		return true
	}
	mimeType := normalizeHTMLMimeType(info.MIMEType)
	for _, prefix := range acceptedHTMLMimeTypePrefixes {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}

func (c *HTMLConverter) Convert(reader io.Reader, info StreamInfo) (DocumentConverterResult, error) {
	if c == nil {
		c = NewHTMLConverter()
	}
	if reader == nil {
		return DocumentConverterResult{}, errors.New("html converter: nil reader")
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return DocumentConverterResult{}, err
	}
	decoded, err := decodeHTML(raw, info.Charset)
	if err != nil {
		return DocumentConverterResult{}, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(decoded))
	if err != nil {
		return DocumentConverterResult{}, err
	}
	doc.Find("script, style").Each(func(_ int, selection *goquery.Selection) {
		selection.Remove()
	})
	c.cleanImages(doc)
	c.normalizeInputs(doc)
	title := strings.TrimSpace(doc.Find("title").First().Text())
	target := doc.Find("body").First()
	if target.Length() == 0 {
		target = doc.Selection
	}
	htmlFragment, err := target.Html()
	if err != nil {
		return DocumentConverterResult{}, err
	}
	markdown, err := c.markdown.ConvertString(htmlFragment)
	if err != nil {
		markdown = plainHTMLText(target)
	}
	markdown = normalizeHTMLMarkdown(markdown)
	return DocumentConverterResult{Markdown: strings.TrimSpace(markdown), Title: title}, nil
}

func (c *HTMLConverter) ConvertString(htmlContent string, info StreamInfo) (DocumentConverterResult, error) {
	if info.MIMEType == "" {
		info.MIMEType = "text/html"
	}
	if info.Extension == "" {
		info.Extension = ".html"
	}
	if info.Charset == "" {
		info.Charset = "utf-8"
	}
	return c.Convert(strings.NewReader(htmlContent), info)
}

func (c *HTMLConverter) cleanImages(doc *goquery.Document) {
	doc.Find("img").Each(func(_ int, selection *goquery.Selection) {
		src, exists := selection.Attr("src")
		if !exists || src == "" {
			src, exists = selection.Attr("data-src")
			if exists {
				selection.SetAttr("src", src)
			}
		}
		if strings.HasPrefix(src, "data:") {
			comma := strings.Index(src, ",")
			if comma >= 0 {
				selection.SetAttr("src", src[:comma]+"...")
			} else {
				selection.SetAttr("src", "data:...")
			}
		}
		alt, exists := selection.Attr("alt")
		if exists {
			selection.SetAttr("alt", strings.ReplaceAll(alt, "\n", " "))
		}
	})
}

func (c *HTMLConverter) normalizeInputs(doc *goquery.Document) {
	doc.Find("input[type='checkbox']").Each(func(_ int, selection *goquery.Selection) {
		replacement := "[ ] "
		if _, checked := selection.Attr("checked"); checked {
			replacement = "[x] "
		}
		selection.ReplaceWithHtml(replacement)
	})
}

func decodeHTML(raw []byte, requestedCharset string) (string, error) {
	if strings.TrimSpace(requestedCharset) == "" {
		requestedCharset = "utf-8"
	}
	encoding, _, _ := charset.DetermineEncoding(raw, requestedCharset)
	reader := encoding.NewDecoder().Reader(bytes.NewReader(raw))
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func normalizeHTMLMimeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(value)
	if err == nil {
		return strings.ToLower(parsed)
	}
	if index := strings.Index(value, ";"); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
}

func htmlCharsetFromContentType(value string) string {
	_, params, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(params["charset"])
}

func plainHTMLText(selection *goquery.Selection) string {
	lines := make([]string, 0)
	selection.Each(func(_ int, item *goquery.Selection) {
		text := strings.TrimSpace(item.Text())
		if text == "" {
			return
		}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				lines = append(lines, line)
			}
		}
	})
	return strings.Join(lines, "\n")
}

func CleanHTMLForLLM(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return CollapseWhitespace(StripHTML(raw))
	}
	doc.Find("script, style, svg, noscript, iframe").Each(func(_ int, selection *goquery.Selection) {
		selection.Remove()
	})
	doc.Find("meta").Each(func(_ int, selection *goquery.Selection) {
		name, _ := selection.Attr("name")
		property, _ := selection.Attr("property")
		name = strings.ToLower(strings.TrimSpace(name))
		property = strings.ToLower(strings.TrimSpace(property))
		if name != "description" && !strings.HasPrefix(property, "og:") {
			selection.Remove()
		}
	})
	doc.Find("[style], [onclick], [class], [id]").Each(func(_ int, selection *goquery.Selection) {
		selection.RemoveAttr("style")
		selection.RemoveAttr("onclick")
		selection.RemoveAttr("class")
		selection.RemoveAttr("id")
	})
	main := firstNonEmptySelection(
		doc.Find("main").First(),
		doc.Find("article").First(),
		doc.Find("[role='main']").First(),
		doc.Find("body").First(),
		doc.Selection,
	)
	return CollapseWhitespace(visibleSelectionText(main))
}

func firstNonEmptySelection(selections ...*goquery.Selection) *goquery.Selection {
	for _, selection := range selections {
		if selection == nil || selection.Length() == 0 {
			continue
		}
		if strings.TrimSpace(selection.Text()) != "" {
			return selection
		}
	}
	for _, selection := range selections {
		if selection != nil && selection.Length() > 0 {
			return selection
		}
	}
	return &goquery.Selection{}
}

func CollapseWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

var htmlTextSeparatorTags = map[string]struct{}{
	"address": {}, "article": {}, "aside": {}, "blockquote": {}, "br": {}, "dd": {}, "div": {},
	"dl": {}, "dt": {}, "fieldset": {}, "figcaption": {}, "figure": {}, "footer": {}, "form": {},
	"h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {}, "header": {}, "hr": {},
	"li": {}, "main": {}, "nav": {}, "ol": {}, "p": {}, "pre": {}, "section": {}, "table": {},
	"td": {}, "th": {}, "tr": {}, "ul": {},
}

func visibleSelectionText(selection *goquery.Selection) string {
	if selection == nil || len(selection.Nodes) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, node := range selection.Nodes {
		appendVisibleNodeText(&builder, node)
	}
	return builder.String()
}

func appendVisibleNodeText(builder *strings.Builder, node *html.Node) {
	if node == nil {
		return
	}
	switch node.Type {
	case html.TextNode:
		builder.WriteString(node.Data)
	case html.ElementNode:
		if _, ok := htmlTextSeparatorTags[strings.ToLower(node.Data)]; ok {
			builder.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			appendVisibleNodeText(builder, child)
		}
		if _, ok := htmlTextSeparatorTags[strings.ToLower(node.Data)]; ok {
			builder.WriteByte(' ')
		}
	default:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			appendVisibleNodeText(builder, child)
		}
	}
}

func normalizeHTMLMarkdown(markdown string) string {
	markdown = strings.ReplaceAll(markdown, `\[x\]`, `[x]`)
	markdown = strings.ReplaceAll(markdown, `\[X\]`, `[x]`)
	markdown = strings.ReplaceAll(markdown, `\[ \]`, `[ ]`)
	return markdown
}
