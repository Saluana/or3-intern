package tools

import (
	"strings"
	"testing"
)

func TestHTMLConverterAcceptsExtension(t *testing.T) {
	converter := NewHTMLConverter()
	if !converter.Accepts(StreamInfo{Extension: ".html"}) {
		t.Fatal("expected .html to be accepted")
	}
	if !converter.Accepts(StreamInfo{Extension: ".htm"}) {
		t.Fatal("expected .htm to be accepted")
	}
}

func TestHTMLConverterAcceptsMimeType(t *testing.T) {
	converter := NewHTMLConverter()
	if !converter.Accepts(StreamInfo{MIMEType: "text/html; charset=utf-8"}) {
		t.Fatal("expected text/html to be accepted")
	}
	if !converter.Accepts(StreamInfo{MIMEType: "application/xhtml+xml"}) {
		t.Fatal("expected application/xhtml+xml to be accepted")
	}
}

func TestHTMLConverterStripsScriptAndStyle(t *testing.T) {
	converter := NewHTMLConverter()
	result, err := converter.ConvertString(`
		<html>
			<head>
				<title>Hello</title>
				<style>.x { color: red; }</style>
				<script>alert("bad")</script>
			</head>
			<body>
				<h1>Main</h1>
				<p>Body text</p>
			</body>
		</html>
	`, StreamInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Hello" {
		t.Fatalf("expected title Hello, got %q", result.Title)
	}
	if strings.Contains(result.Markdown, "alert") {
		t.Fatal("script content should be removed")
	}
	if strings.Contains(result.Markdown, "color: red") {
		t.Fatal("style content should be removed")
	}
	if !strings.Contains(result.Markdown, "Main") {
		t.Fatal("expected body heading in markdown")
	}
}

func TestHTMLConverterCheckboxes(t *testing.T) {
	converter := NewHTMLConverter()
	result, err := converter.ConvertString(`
		<body>
			<p><input type="checkbox" checked> Done</p>
			<p><input type="checkbox"> Todo</p>
		</body>
	`, StreamInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Markdown, "[x]") {
		t.Fatal("checked checkbox should become [x]")
	}
	if !strings.Contains(result.Markdown, "[ ]") {
		t.Fatal("unchecked checkbox should become [ ]")
	}
}

func TestHTMLConverterRemovesDataURIImagePayload(t *testing.T) {
	converter := NewHTMLConverter()
	result, err := converter.ConvertString(`
		<body>
			<img alt="Example
Image" src="data:image/png;base64,AAAAAA">
		</body>
	`, StreamInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Markdown, "AAAAAA") {
		t.Fatal("data URI payload should be stripped")
	}
	if !strings.Contains(result.Markdown, "data:image/png;base64...") {
		t.Fatal("expected shortened data URI")
	}
}

func TestCleanHTMLForLLMRemovesNoiseAndKeepsMainText(t *testing.T) {
	cleaned := CleanHTMLForLLM(`
		<html>
			<head>
				<meta charset="utf-8">
				<meta name="description" content="keep me">
				<style>.hidden{display:none}</style>
				<script>alert("bad")</script>
			</head>
			<body>
				<nav class="menu">Ignore nav</nav>
				<main id="app" class="shell" style="color:red" onclick="hack()">
					<h1>Hello</h1>
					<svg><text>vector noise</text></svg>
					<p>Readable body.</p>
					<iframe src="https://example.com/embed"></iframe>
				</main>
			</body>
		</html>
	`)
	if strings.Contains(cleaned, "alert") || strings.Contains(cleaned, "vector noise") || strings.Contains(cleaned, "hack") {
		t.Fatalf("expected noisy content removed, got %q", cleaned)
	}
	if !strings.Contains(cleaned, "Hello Readable body.") {
		t.Fatalf("expected main visible text preserved, got %q", cleaned)
	}
}
