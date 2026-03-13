package services

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// ExtractTextFromFile dispatches to the correct parser based on file extension.
// Returns clean plain-text content ready to be used as source for question generation.
func ExtractTextFromFile(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return extractPDF(data)
	case ".docx":
		return extractDOCX(data)
	case ".txt", ".gift", ".md":
		return sanitizeText(string(data)), nil
	case ".xlsx":
		return "", fmt.Errorf("file XLSX là template câu hỏi — hãy dùng chức năng Import Template để nhập")
	default:
		// Attempt to treat as plain text anyway
		text := sanitizeText(string(data))
		if text == "" {
			return "", fmt.Errorf("unsupported file type: %s", ext)
		}
		return text, nil
	}
}

// ─────────────────────────────────────────────────────────────
// PDF parser (github.com/ledongthuc/pdf)
// ─────────────────────────────────────────────────────────────

func extractPDF(data []byte) (string, error) {
	r := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(r, int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}

	var sb strings.Builder
	totalPages := pdfReader.NumPage()

	for i := 1; i <= totalPages; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}
		content, err := page.GetPlainText(nil)
		if err != nil {
			continue // skip bad pages, don't fail
		}
		sb.WriteString(content)
		sb.WriteString("\n")
	}

	text := sanitizeText(sb.String())
	if text == "" {
		return "", fmt.Errorf("PDF appears to be scanned/image-based — no extractable text found")
	}
	return text, nil
}

// ─────────────────────────────────────────────────────────────
// DOCX parser (pure Go — reads word/document.xml from ZIP)
// ─────────────────────────────────────────────────────────────

type docxBody struct {
	Paragraphs []docxParagraph `xml:"body>p"`
}

type docxParagraph struct {
	Runs []docxRun `xml:"r"`
}

type docxRun struct {
	Text docxText `xml:"t"`
}

type docxText struct {
	Value    string `xml:",chardata"`
	XMLSpace string `xml:"space,attr"`
}

func extractDOCX(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open DOCX (ZIP): %w", err)
	}

	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open document.xml: %w", err)
		}
		defer rc.Close()

		xmlBytes, err := io.ReadAll(rc)
		if err != nil {
			return "", fmt.Errorf("read document.xml: %w", err)
		}

		var body docxBody
		if err := xml.Unmarshal(xmlBytes, &body); err != nil {
			// Fallback: strip XML tags manually
			return sanitizeText(stripXMLTags(string(xmlBytes))), nil
		}

		var sb strings.Builder
		for _, para := range body.Paragraphs {
			var line strings.Builder
			for _, run := range para.Runs {
				line.WriteString(run.Text.Value)
			}
			text := strings.TrimSpace(line.String())
			if text != "" {
				sb.WriteString(text)
				sb.WriteString("\n")
			}
		}

		result := sanitizeText(sb.String())
		if result == "" {
			return "", fmt.Errorf("DOCX has no readable text content")
		}
		return result, nil
	}
	return "", fmt.Errorf("word/document.xml not found in DOCX file")
}

// ─────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────

var xmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripXMLTags(s string) string {
	return xmlTagRe.ReplaceAllString(s, " ")
}

// sanitizeText removes non-printable characters, normalises whitespace,
// and truncates to 15000 characters (enough context for Claude).
func sanitizeText(s string) string {
	// Remove null bytes and other control chars except newlines/tabs
	var sb strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || r == '\r' || unicode.IsPrint(r) {
			sb.WriteRune(r)
		}
	}

	// Collapse runs of blank lines
	multiBlank := regexp.MustCompile(`\n{3,}`)
	clean := multiBlank.ReplaceAllString(sb.String(), "\n\n")
	clean = strings.TrimSpace(clean)

	// Truncate to avoid overshooting Claude's context
	const maxRunes = 15000
	runes := []rune(clean)
	if len(runes) > maxRunes {
		clean = string(runes[:maxRunes]) + "\n\n[... nội dung đã được cắt bớt để phù hợp với giới hạn xử lý ...]"
	}
	return clean
}

// FileInfo holds parsed file metadata returned to the frontend.
type FileInfo struct {
	Filename  string `json:"filename"`
	Extension string `json:"extension"`
	SizeBytes int    `json:"size_bytes"`
	Content   string `json:"content"`
	Preview   string `json:"preview"` // first 300 chars
	WordCount int    `json:"word_count"`
}

func BuildFileInfo(filename string, data []byte, content string) FileInfo {
	preview := content
	runes := []rune(content)
	if len(runes) > 300 {
		preview = string(runes[:300]) + "..."
	}
	wordCount := len(strings.Fields(content))
	return FileInfo{
		Filename:  filename,
		Extension: strings.ToLower(filepath.Ext(filename)),
		SizeBytes: len(data),
		Content:   content,
		Preview:   preview,
		WordCount: wordCount,
	}
}

// ─────────────────────────────────────────────────────────────
// HTML text extractor (for URL fetch feature)
// ─────────────────────────────────────────────────────────────

var (
	scriptRe   = regexp.MustCompile(`(?si)<script[^>]*>.*?</script>`)
	styleRe    = regexp.MustCompile(`(?si)<style[^>]*>.*?</style>`)
	tagRe      = regexp.MustCompile(`<[^>]+>`)
	entityRe   = regexp.MustCompile(`&[a-zA-Z]+;|&#[0-9]+;`)
	titleTagRe = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
)

var htmlEntities = map[string]string{
	"&amp;": "&", "&lt;": "<", "&gt;": ">",
	"&nbsp;": " ", "&quot;": `"`, "&apos;": "'",
	"&mdash;": "—", "&ndash;": "–", "&hellip;": "...",
}

// ExtractTextFromHTML strips HTML markup and returns readable text.
func ExtractTextFromHTML(html string) string {
	s := scriptRe.ReplaceAllString(html, " ")
	s = styleRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	// Decode common entities
	for entity, ch := range htmlEntities {
		s = strings.ReplaceAll(s, entity, ch)
	}
	// Remove remaining numeric entities
	s = entityRe.ReplaceAllString(s, " ")
	return sanitizeText(s)
}

// ExtractHTMLTitle extracts <title> tag content.
func ExtractHTMLTitle(html string) string {
	m := titleTagRe.FindStringSubmatch(html)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
