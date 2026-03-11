package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"math/rand"

	"github.com/google/uuid"
	"github.com/youorg/moodle-gift-generator/internal/models"
)

type AnthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []Message `json:"messages"`
	System    string    `json:"system"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AnthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func GenerateQuestions(req *models.GenerateRequest, userID string) ([]models.Question, string, error) {
	if len(req.QuestionTypes) == 0 {
		req.QuestionTypes = []string{"multiple_choice"}
	}
	if req.Count == 0 {
		req.Count = 5
	}
	if req.Language == "" {
		req.Language = "vi"
	}
	if req.Count > 20 {
		req.Count = 20
	}

	langInstruction := "Respond in Vietnamese (Tiếng Việt)."
	if req.Language == "en" {
		langInstruction = "Respond in English."
	}

	difficultyNote := ""
	switch req.Difficulty {
	case "easy":
		difficultyNote = "Make questions simple and straightforward."
	case "hard":
		difficultyNote = "Make questions challenging and complex."
	default:
		difficultyNote = "Make questions of moderate difficulty."
	}

	typeDescriptions := buildTypeDescriptions(req.QuestionTypes)

	prompt := fmt.Sprintf(`You are an expert teacher creating Moodle GIFT format questions.

SOURCE TEXT:
%s

INSTRUCTIONS:
- Generate exactly %d questions total
- Question types to use: %s
- %s
- %s
- Output ONLY the GIFT format, no explanations, no markdown

GIFT FORMAT RULES:
- Multiple Choice: ::Title:: Question text { =Correct answer ~Wrong1 ~Wrong2 ~Wrong3 }
- True/False: ::Title:: Statement { TRUE } or { FALSE }
- Short Answer: ::Title:: Question { =answer1 =answer2 }
- Essay: ::Title:: Question text {}
- Matching: ::Title:: Match this { =A -> Answer1 =B -> Answer2 =C -> Answer3 }

For multiple choice with feedback use: =Correct answer#Correct! ~Wrong1#Try again

Generate %d diverse, high-quality questions now:`,
		req.SourceText,
		req.Count,
		strings.Join(req.QuestionTypes, ", "),
		langInstruction,
		difficultyNote,
		typeDescriptions,
		req.Count,
	)

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		// Demo mode - generate sample GIFT
		return generateDemoQuestions(req, userID)
	}

	body, err := json.Marshal(AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 4000,
		System:    "You are an expert educator. Output only valid GIFT format questions, nothing else.",
		Messages:  []Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return nil, "", err
	}

	httpReq, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	var apiResp AnthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, "", fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, "", fmt.Errorf("empty response from API: %s", string(respBody))
	}

	giftText := apiResp.Content[0].Text
	questions := parseGIFTToQuestions(giftText, userID, req.BankID, req.Language, req.SourceText)

	return questions, giftText, nil
}

func buildTypeDescriptions(types []string) string {
	descriptions := map[string]string{
		"multiple_choice": "Multiple choice with 4 options",
		"true_false":      "True/False statements",
		"short_answer":    "Short answer fill-in-the-blank",
		"essay":           "Open-ended essay questions",
		"matching":        "Matching pairs",
	}
	var parts []string
	for _, t := range types {
		if desc, ok := descriptions[t]; ok {
			parts = append(parts, desc)
		}
	}
	return strings.Join(parts, ", ")
}

func parseGIFTToQuestions(giftText, userID, bankID, language, sourceText string) []models.Question {
	var questions []models.Question
	blocks := splitGIFTBlocks(giftText)

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		q := models.Question{
			ID:         uuid.New().String(),
			UserID:     userID,
			BankID:     bankID,
			Content:    block,
			SourceText: sourceText,
			Language:   language,
			Tags:       []string{},
		}

		q.Type = detectGIFTType(block)
		q.Title = extractGIFTTitle(block)
		if q.Title == "" {
			q.Title = fmt.Sprintf("Question %s", q.ID[:8])
		}

		questions = append(questions, q)
	}
	return questions
}

func splitGIFTBlocks(text string) []string {
	lines := strings.Split(text, "\n")
	var blocks []string
	var current strings.Builder
	inBlock := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			continue
		}
		if line == "" {
			if inBlock && current.Len() > 0 {
				blocks = append(blocks, current.String())
				current.Reset()
				inBlock = false
			}
			continue
		}
		current.WriteString(line + "\n")
		inBlock = true
	}
	if current.Len() > 0 {
		blocks = append(blocks, current.String())
	}
	return blocks
}

func detectGIFTType(block string) string {
	if strings.Contains(block, "TRUE") || strings.Contains(block, "FALSE") {
		if !strings.Contains(block, "~") {
			return "true_false"
		}
	}
	if strings.Contains(block, "->") {
		return "matching"
	}
	if strings.Contains(block, "~") {
		return "multiple_choice"
	}
	content := extractGIFTContent(block)
	if content == "" {
		return "essay"
	}
	if strings.Contains(block, "{") && !strings.Contains(block, "~") {
		return "short_answer"
	}
	return "essay"
}

func extractGIFTTitle(block string) string {
	if strings.HasPrefix(block, "::") {
		end := strings.Index(block[2:], "::")
		if end > 0 {
			return strings.TrimSpace(block[2 : end+2])
		}
	}
	// Fallback: first 60 chars
	clean := stripGIFTFormatting(block)
	if len(clean) > 60 {
		return clean[:60] + "..."
	}
	return clean
}

func extractGIFTContent(block string) string {
	start := strings.Index(block, "{")
	end := strings.LastIndex(block, "}")
	if start < 0 || end < 0 {
		return ""
	}
	return strings.TrimSpace(block[start+1 : end])
}

func stripGIFTFormatting(block string) string {
	// Remove ::title:: prefix
	if strings.HasPrefix(block, "::") {
		end := strings.Index(block[2:], "::")
		if end > 0 {
			block = block[end+4:]
		}
	}
	// Remove { ... }
	if start := strings.Index(block, "{"); start > 0 {
		block = block[:start]
	}
	return strings.TrimSpace(block)
}

func generateDemoQuestions(req *models.GenerateRequest, userID string) ([]models.Question, string, error) {
	_ = rand.New(rand.NewSource(time.Now().UnixNano()))
	
	demoGIFT := `// Demo questions - Configure ANTHROPIC_API_KEY for AI-generated questions

::Q1:: What is the main topic of this content? {
=The main subject discussed
~An unrelated topic
~A peripheral concept
~None of the above
}

::Q2:: This content provides useful information. { TRUE }

::Q3:: Provide a brief summary of the key concept. {}

::Q4:: The term used to describe this concept is ___. { =keyword =key term }

::Q5:: Match the following concepts. {
=Concept A -> Definition 1
=Concept B -> Definition 2
=Concept C -> Definition 3
}`

	questions := parseGIFTToQuestions(demoGIFT, userID, req.BankID, req.Language, req.SourceText)
	return questions, demoGIFT, nil
}

// ParseGIFTFile parses an uploaded GIFT file into questions
func ParseGIFTFile(content, userID, bankID string) ([]models.Question, error) {
	questions := parseGIFTToQuestions(content, userID, bankID, "vi", "")
	if len(questions) == 0 {
		return nil, fmt.Errorf("no valid GIFT questions found in the file")
	}
	return questions, nil
}
