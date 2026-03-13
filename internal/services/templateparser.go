// Package services — Template parser: reads structured TXT/XLSX template files
// and converts them to Moodle GIFT format.
//
// Supported question types (per Moodle GIFT spec):
//
//	MC    – Multiple Choice (single correct)
//	MA    – Multiple Answers (multiple correct with percentage weights)
//	TF    – True / False
//	SA    – Short Answer
//	MATCH – Matching
//	MW    – Missing Word (fill-in-the-blank inside a sentence)
//	NUM   – Numerical (with tolerance or range)
//	ESSAY – Essay (open-ended, no answer)
//	DESC  – Description (informational text, no answer)
package services

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ─────────────────────────────────────────────────────────────────────────────
// Types
// ─────────────────────────────────────────────────────────────────────────────

// TemplateQuestion holds a single question parsed from a template file.
type TemplateQuestion struct {
	Type     string // MC, TF, SA, MATCH, NUM, ESSAY, MW, MA, DESC
	Title    string
	Question string
	Category string

	// MC, MW, SA
	Correct []string
	Wrong   []string

	// TF
	TFAnswer string // "TRUE" or "FALSE"

	// MATCH
	Pairs []MatchPair

	// NUM
	NumAnswer float64
	Tolerance float64
	MinRange  float64
	MaxRange  float64
	UseRange  bool

	// MA
	MAAnswers []MAAnswer

	// Feedback
	Feedback        string
	CorrectFeedback string
	WrongFeedback   string
}

// MatchPair is a term → match pair for matching questions.
type MatchPair struct {
	Term  string
	Match string
}

// MAAnswer is an answer with a percentage weight for multiple-answer questions.
type MAAnswer struct {
	Text   string
	Weight float64 // e.g. 50, 33.33333, -100
}

// ─────────────────────────────────────────────────────────────────────────────
// TXT Template Parser
// ─────────────────────────────────────────────────────────────────────────────

// ParseTemplateTXT parses questions from the structured TXT template format.
func ParseTemplateTXT(data []byte) ([]TemplateQuestion, error) {
	lines := strings.Split(string(data), "\n")
	var questions []TemplateQuestion
	var current *TemplateQuestion

	flush := func() {
		if current != nil && current.Type != "" {
			questions = append(questions, *current)
		}
		current = nil
	}

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r\n")
		trimmed := strings.TrimSpace(line)

		// Skip comments
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Blank line → end current question
		if trimmed == "" {
			flush()
			continue
		}

		// Parse KEY: VALUE
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx < 0 {
			continue
		}

		key := strings.ToUpper(strings.TrimSpace(trimmed[:colonIdx]))
		value := strings.TrimSpace(trimmed[colonIdx+1:])

		if key == "TYPE" {
			flush()
			current = &TemplateQuestion{Type: strings.ToUpper(value)}
			continue
		}

		if current == nil {
			continue
		}

		switch key {
		case "TITLE":
			current.Title = value
		case "QUESTION", "Q":
			current.Question = value
		case "CATEGORY":
			current.Category = value
		case "CORRECT":
			current.Correct = append(current.Correct, value)
		case "WRONG":
			current.Wrong = append(current.Wrong, value)
		case "ANSWER":
			switch current.Type {
			case "TF":
				current.TFAnswer = strings.ToUpper(value)
			case "SA":
				current.Correct = append(current.Correct, value)
			case "NUM":
				current.NumAnswer, _ = strconv.ParseFloat(value, 64)
			case "MA":
				parts := strings.SplitN(value, "|", 2)
				text := strings.TrimSpace(parts[0])
				weight := 0.0
				if len(parts) > 1 {
					weight, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				}
				current.MAAnswers = append(current.MAAnswers, MAAnswer{Text: text, Weight: weight})
			}
		case "TOLERANCE":
			current.Tolerance, _ = strconv.ParseFloat(value, 64)
		case "MIN":
			current.MinRange, _ = strconv.ParseFloat(value, 64)
			current.UseRange = true
		case "MAX":
			current.MaxRange, _ = strconv.ParseFloat(value, 64)
			current.UseRange = true
		case "PAIR":
			parts := strings.SplitN(value, "->", 2)
			if len(parts) == 2 {
				current.Pairs = append(current.Pairs, MatchPair{
					Term:  strings.TrimSpace(parts[0]),
					Match: strings.TrimSpace(parts[1]),
				})
			}
		case "FEEDBACK":
			current.Feedback = value
		case "CORRECT_FEEDBACK":
			current.CorrectFeedback = value
		case "WRONG_FEEDBACK":
			current.WrongFeedback = value
		}
	}
	flush()

	if len(questions) == 0 {
		return nil, fmt.Errorf("không tìm thấy câu hỏi hợp lệ trong file template")
	}
	return questions, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// XLSX Template Parser
// ─────────────────────────────────────────────────────────────────────────────

// ParseTemplateXLSX parses questions from an XLSX template file.
// Each sheet represents a question type, identified by prefix (e.g. "MC - …").
func ParseTemplateXLSX(data []byte) ([]TemplateQuestion, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("không thể mở file xlsx: %w", err)
	}
	defer f.Close()

	var questions []TemplateQuestion

	for _, sheetName := range f.GetSheetList() {
		qType := detectSheetType(sheetName)
		if qType == "" {
			continue
		}

		rows, err := f.GetRows(sheetName)
		if err != nil || len(rows) < 2 {
			continue // need header + at least 1 data row
		}

		// rows[0] = header, rows[1:] = data
		for _, row := range rows[1:] {
			q := parseXLSXRow(qType, row)
			if q != nil {
				questions = append(questions, *q)
			}
		}
	}

	if len(questions) == 0 {
		return nil, fmt.Errorf("không tìm thấy câu hỏi hợp lệ trong file xlsx")
	}
	return questions, nil
}

func detectSheetType(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	prefixes := []struct {
		prefix string
		qtype  string
	}{
		{"MC", "MC"},
		{"MA", "MA"},
		{"TF", "TF"},
		{"SA", "SA"},
		{"MATCH", "MATCH"},
		{"NUM", "NUM"},
		{"ESSAY", "ESSAY"},
		{"MW", "MW"},
		{"DESC", "DESC"},
	}
	for _, p := range prefixes {
		if strings.HasPrefix(upper, p.prefix+" ") || strings.HasPrefix(upper, p.prefix+"-") || upper == p.prefix {
			return p.qtype
		}
	}
	return ""
}

func cellVal(row []string, idx int) string {
	if idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

func parseXLSXRow(qType string, row []string) *TemplateQuestion {
	q := &TemplateQuestion{Type: qType}

	switch qType {
	case "MC":
		// Columns: Title | Question | Correct | Wrong1 | Wrong2 | Wrong3 | Feedback
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" {
			return nil
		}
		if c := cellVal(row, 2); c != "" {
			q.Correct = append(q.Correct, c)
		}
		for i := 3; i <= 5; i++ {
			if w := cellVal(row, i); w != "" {
				q.Wrong = append(q.Wrong, w)
			}
		}
		q.Feedback = cellVal(row, 6)

	case "TF":
		// Columns: Title | Statement | Answer(TRUE/FALSE) | Feedback
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" {
			return nil
		}
		q.TFAnswer = strings.ToUpper(cellVal(row, 2))
		if q.TFAnswer != "TRUE" && q.TFAnswer != "FALSE" {
			q.TFAnswer = "TRUE"
		}
		q.Feedback = cellVal(row, 3)

	case "SA":
		// Columns: Title | Question | Answer1 | Answer2 | Answer3 | Answer4
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" {
			return nil
		}
		for i := 2; i <= 5; i++ {
			if a := cellVal(row, i); a != "" {
				q.Correct = append(q.Correct, a)
			}
		}

	case "MATCH":
		// Columns: Title | Question | Term1 | Match1 | Term2 | Match2 | Term3 | Match3 | Term4 | Match4
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" {
			return nil
		}
		for i := 2; i+1 < len(row) && i <= 10; i += 2 {
			t := cellVal(row, i)
			m := cellVal(row, i+1)
			if t != "" && m != "" {
				q.Pairs = append(q.Pairs, MatchPair{Term: t, Match: m})
			}
		}
		if len(q.Pairs) < 3 {
			return nil
		}

	case "NUM":
		// Columns: Title | Question | Answer | Tolerance | Min | Max
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" {
			return nil
		}
		if a := cellVal(row, 2); a != "" {
			q.NumAnswer, _ = strconv.ParseFloat(a, 64)
		}
		if t := cellVal(row, 3); t != "" {
			q.Tolerance, _ = strconv.ParseFloat(t, 64)
		}
		if mn := cellVal(row, 4); mn != "" {
			q.MinRange, _ = strconv.ParseFloat(mn, 64)
			q.UseRange = true
		}
		if mx := cellVal(row, 5); mx != "" {
			q.MaxRange, _ = strconv.ParseFloat(mx, 64)
			q.UseRange = true
		}

	case "ESSAY":
		// Columns: Title | Question
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" {
			return nil
		}

	case "MW":
		// Columns: Title | Question (use {} for blank) | Correct | Wrong1 | Wrong2 | Wrong3
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" || !strings.Contains(q.Question, "{}") {
			return nil
		}
		if c := cellVal(row, 2); c != "" {
			q.Correct = append(q.Correct, c)
		}
		for i := 3; i <= 5; i++ {
			if w := cellVal(row, i); w != "" {
				q.Wrong = append(q.Wrong, w)
			}
		}

	case "MA":
		// Columns: Title | Question | Ans1 | Weight1 | Ans2 | Weight2 | Ans3 | Weight3 | Ans4 | Weight4 | Ans5 | Weight5
		q.Title = cellVal(row, 0)
		q.Question = cellVal(row, 1)
		if q.Question == "" {
			return nil
		}
		for i := 2; i+1 < len(row) && i <= 12; i += 2 {
			text := cellVal(row, i)
			weightStr := cellVal(row, i+1)
			if text != "" {
				w, _ := strconv.ParseFloat(weightStr, 64)
				q.MAAnswers = append(q.MAAnswers, MAAnswer{Text: text, Weight: w})
			}
		}

	case "DESC":
		// Columns: Content
		q.Question = cellVal(row, 0)
		if q.Question == "" {
			return nil
		}
	}

	return q
}

// ─────────────────────────────────────────────────────────────────────────────
// GIFT Converter
// ─────────────────────────────────────────────────────────────────────────────

// ConvertToGIFT converts parsed template questions to a complete GIFT format string.
func ConvertToGIFT(questions []TemplateQuestion) string {
	var sb strings.Builder
	sb.WriteString("// GIFT format — converted from template\n")
	sb.WriteString(fmt.Sprintf("// Total: %d questions\n\n", len(questions)))

	lastCategory := ""

	for _, q := range questions {
		if q.Category != "" && q.Category != lastCategory {
			sb.WriteString(fmt.Sprintf("$CATEGORY: %s\n\n", q.Category))
			lastCategory = q.Category
		}
		gift := convertOneToGIFT(q)
		if gift != "" {
			sb.WriteString(gift)
			sb.WriteString("\n\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// convertOneToGIFT converts a single TemplateQuestion to GIFT format.
func convertOneToGIFT(q TemplateQuestion) string {
	title := q.Title
	if title == "" {
		r := []rune(q.Question)
		if len(r) > 50 {
			title = string(r[:50]) + "..."
		} else {
			title = q.Question
		}
	}

	switch q.Type {
	case "MC":
		return convertMCtoGIFT(title, q)
	case "TF":
		return convertTFtoGIFT(title, q)
	case "SA":
		return convertSAtoGIFT(title, q)
	case "MATCH":
		return convertMATCHtoGIFT(title, q)
	case "NUM":
		return convertNUMtoGIFT(title, q)
	case "ESSAY":
		return convertESSAYtoGIFT(title, q)
	case "MW":
		return convertMWtoGIFT(title, q)
	case "MA":
		return convertMAtoGIFT(title, q)
	case "DESC":
		return q.Question // Description has no answer part
	default:
		return ""
	}
}

func convertMCtoGIFT(title string, q TemplateQuestion) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "::%s::%s {\n", escapeGIFTTitle(title), escapeGIFT(q.Question))
	for _, c := range q.Correct {
		sb.WriteString("  =")
		sb.WriteString(escapeGIFT(c))
		if q.CorrectFeedback != "" {
			fmt.Fprintf(&sb, " #%s", escapeGIFT(q.CorrectFeedback))
		}
		sb.WriteString("\n")
	}
	for _, w := range q.Wrong {
		sb.WriteString("  ~")
		sb.WriteString(escapeGIFT(w))
		if q.WrongFeedback != "" {
			fmt.Fprintf(&sb, " #%s", escapeGIFT(q.WrongFeedback))
		}
		sb.WriteString("\n")
	}
	if q.Feedback != "" {
		fmt.Fprintf(&sb, "  ####%s\n", escapeGIFT(q.Feedback))
	}
	sb.WriteString("}")
	return sb.String()
}

func convertTFtoGIFT(title string, q TemplateQuestion) string {
	answer := "TRUE"
	if q.TFAnswer == "FALSE" || q.TFAnswer == "F" {
		answer = "FALSE"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "::%s::%s {%s", escapeGIFTTitle(title), escapeGIFT(q.Question), answer)
	if q.Feedback != "" {
		fmt.Fprintf(&sb, " #%s", escapeGIFT(q.Feedback))
	}
	sb.WriteString("}")
	return sb.String()
}

func convertSAtoGIFT(title string, q TemplateQuestion) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "::%s::%s {\n", escapeGIFTTitle(title), escapeGIFT(q.Question))
	for _, a := range q.Correct {
		fmt.Fprintf(&sb, "  =%s\n", escapeGIFT(a))
	}
	sb.WriteString("}")
	return sb.String()
}

func convertMATCHtoGIFT(title string, q TemplateQuestion) string {
	if len(q.Pairs) < 3 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "::%s::%s {\n", escapeGIFTTitle(title), escapeGIFT(q.Question))
	for _, p := range q.Pairs {
		fmt.Fprintf(&sb, "  =%s -> %s\n", escapeGIFT(p.Term), escapeGIFT(p.Match))
	}
	sb.WriteString("}")
	return sb.String()
}

func convertNUMtoGIFT(title string, q TemplateQuestion) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "::%s::%s {#", escapeGIFTTitle(title), escapeGIFT(q.Question))
	if q.UseRange {
		fmt.Fprintf(&sb, "%s..%s", formatNum(q.MinRange), formatNum(q.MaxRange))
	} else {
		sb.WriteString(formatNum(q.NumAnswer))
		if q.Tolerance > 0 {
			fmt.Fprintf(&sb, ":%s", formatNum(q.Tolerance))
		}
	}
	sb.WriteString("}")
	return sb.String()
}

func convertESSAYtoGIFT(title string, q TemplateQuestion) string {
	return fmt.Sprintf("::%s::%s {}", escapeGIFTTitle(title), escapeGIFT(q.Question))
}

func convertMWtoGIFT(title string, q TemplateQuestion) string {
	// Missing word: question contains {} as placeholder
	// GIFT format: text before {=correct ~wrong} text after
	parts := strings.SplitN(q.Question, "{}", 2)
	if len(parts) != 2 {
		return ""
	}
	before := strings.TrimSpace(parts[0])
	after := strings.TrimSpace(parts[1])

	var sb strings.Builder
	fmt.Fprintf(&sb, "::%s::%s {", escapeGIFTTitle(title), escapeGIFT(before))
	for _, c := range q.Correct {
		fmt.Fprintf(&sb, "=%s ", escapeGIFT(c))
	}
	for _, w := range q.Wrong {
		fmt.Fprintf(&sb, "~%s ", escapeGIFT(w))
	}
	sb.WriteString("} ")
	sb.WriteString(escapeGIFT(after))
	return sb.String()
}

func convertMAtoGIFT(title string, q TemplateQuestion) string {
	if len(q.MAAnswers) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "::%s::%s {\n", escapeGIFTTitle(title), escapeGIFT(q.Question))
	for _, a := range q.MAAnswers {
		w := formatNum(a.Weight)
		fmt.Fprintf(&sb, "  ~%%%s%%%s\n", w, escapeGIFT(a.Text))
	}
	sb.WriteString("}")
	return sb.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// GIFT Helpers
// ─────────────────────────────────────────────────────────────────────────────

func escapeGIFTTitle(s string) string {
	// Title is between :: :: so we escape : inside it
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ":", "\\:")
	return s
}

func formatNum(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// TemplateQuestionToDBType maps template type to database question type.
func TemplateQuestionToDBType(tplType string) string {
	m := map[string]string{
		"MC":    "multiple_choice",
		"TF":    "true_false",
		"SA":    "short_answer",
		"MATCH": "matching",
		"NUM":   "numerical",
		"ESSAY": "essay",
		"MW":    "missing_word",
		"MA":    "multiple_answer",
		"DESC":  "description",
	}
	if t, ok := m[tplType]; ok {
		return t
	}
	return "multiple_choice"
}

// ─────────────────────────────────────────────────────────────────────────────
// XLSX Template Generator — creates a sample template file
// ─────────────────────────────────────────────────────────────────────────────

// GenerateTemplateXLSX creates a sample XLSX template with all question types.
func GenerateTemplateXLSX() (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	// Styles
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"2563EB"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "bottom", Color: "1E40AF", Style: 2},
		},
	})
	sampleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Color: "6B7280", Italic: true, Size: 10},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F3F4F6"}},
	})

	// ─── Instructions sheet ───
	f.SetSheetName("Sheet1", "Hướng dẫn")
	instructions := []string{
		"HƯỚNG DẪN SỬ DỤNG TEMPLATE IMPORT CÂU HỎI",
		"",
		"1. Mỗi sheet tương ứng với 1 loại câu hỏi Moodle GIFT",
		"2. Dòng 1 là tiêu đề cột - KHÔNG SỬA",
		"3. Dòng 2 là dữ liệu mẫu (có thể xóa hoặc ghi đè)",
		"4. Điền câu hỏi từ dòng 2 trở đi",
		"5. Lưu file và upload vào hệ thống",
		"6. Hệ thống sẽ tự động chuyển đổi sang GIFT format",
		"",
		"CÁC LOẠI CÂU HỎI:",
		"  MC  - Trắc nghiệm (1 đáp án đúng)",
		"  TF  - Đúng / Sai",
		"  SA  - Điền từ ngắn (Short Answer)",
		"  MATCH - Nối cặp (tối thiểu 3 cặp)",
		"  NUM - Câu hỏi số (với sai số hoặc khoảng)",
		"  ESSAY - Tự luận",
		"  MW  - Điền vào chỗ trống (Missing Word)",
		"  MA  - Nhiều đáp án đúng (Multiple Answers)",
		"  DESC - Mô tả (không có đáp án)",
		"",
		"LƯU Ý:",
		"  - File phải lưu dạng .xlsx (Excel)",
		"  - Chỉ cần điền các sheet có câu hỏi, bỏ qua sheet trống",
		"  - Matching cần tối thiểu 3 cặp",
		"  - Missing Word: dùng {} đánh dấu chỗ trống trong câu hỏi",
		"  - Multiple Answers: tổng trọng số đáp án đúng = 100%",
	}
	for i, line := range instructions {
		f.SetCellValue("Hướng dẫn", fmt.Sprintf("A%d", i+1), line)
	}
	f.SetColWidth("Hướng dẫn", "A", "A", 80)

	// ─── MC sheet ───
	createSheet(f, "MC - Trắc nghiệm", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Câu hỏi", "Đáp án đúng", "Đáp án sai 1", "Đáp án sai 2", "Đáp án sai 3", "Phản hồi chung"},
		[][]string{
			{"Thủ đô Việt Nam", "Thủ đô của Việt Nam là thành phố nào?", "Hà Nội", "TP. Hồ Chí Minh", "Đà Nẵng", "Huế", "Hà Nội là thủ đô của Việt Nam từ năm 1010"},
			{"Hành tinh lớn nhất", "Hành tinh lớn nhất trong hệ Mặt Trời là gì?", "Sao Mộc", "Sao Thổ", "Sao Hỏa", "Sao Kim", ""},
		},
		[]float64{25, 45, 20, 20, 20, 20, 35},
	)

	// ─── TF sheet ───
	createSheet(f, "TF - Đúng Sai", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Phát biểu", "Đáp án (TRUE/FALSE)", "Phản hồi"},
		[][]string{
			{"Trái Đất quay quanh Mặt Trời", "Trái Đất quay quanh Mặt Trời.", "TRUE", "Đúng! Trái Đất quay quanh Mặt Trời theo quỹ đạo elip"},
			{"Mặt Trời quay quanh Trái Đất", "Mặt Trời quay quanh Trái Đất.", "FALSE", "Sai! Trái Đất quay quanh Mặt Trời, không phải ngược lại"},
		},
		[]float64{30, 50, 20, 40},
	)

	// ─── SA sheet ───
	createSheet(f, "SA - Điền từ", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Câu hỏi", "Đáp án 1", "Đáp án 2", "Đáp án 3", "Đáp án 4"},
		[][]string{
			{"Thủ đô Pháp", "Thủ đô của nước Pháp là gì?", "Paris", "paris", "PARIS", ""},
			{"2 + 2", "Hai cộng hai bằng mấy?", "4", "bốn", "Bốn", "four"},
		},
		[]float64{25, 45, 20, 20, 20, 20},
	)

	// ─── MATCH sheet ───
	createSheet(f, "MATCH - Nối cặp", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Câu hỏi", "Thuật ngữ 1", "Kết quả 1", "Thuật ngữ 2", "Kết quả 2", "Thuật ngữ 3", "Kết quả 3", "Thuật ngữ 4", "Kết quả 4"},
		[][]string{
			{"Thủ đô các nước", "Nối quốc gia với thủ đô tương ứng", "Việt Nam", "Hà Nội", "Nhật Bản", "Tokyo", "Pháp", "Paris", "Đức", "Berlin"},
			{"Ngôn ngữ lập trình", "Nối ngôn ngữ với đặc điểm", "Python", "Ngôn ngữ thông dịch", "Java", "Write once run anywhere", "C", "Lập trình hệ thống", "", ""},
		},
		[]float64{25, 35, 18, 18, 18, 18, 18, 18, 18, 18},
	)

	// ─── NUM sheet ───
	createSheet(f, "NUM - Câu hỏi số", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Câu hỏi", "Đáp án", "Sai số (±)", "Giá trị Min", "Giá trị Max"},
		[][]string{
			{"Giá trị Pi", "Giá trị của số Pi (đến 3 chữ số thập phân)?", "3.14159", "0.0005", "", ""},
			{"Số trong khoảng", "Cho một số nguyên tố nhỏ hơn 10", "", "", "2", "7"},
		},
		[]float64{25, 45, 15, 15, 15, 15},
	)

	// ─── ESSAY sheet ───
	createSheet(f, "ESSAY - Tự luận", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Câu hỏi"},
		[][]string{
			{"Nguyên nhân WWII", "Hãy trình bày nguyên nhân chính dẫn đến Chiến tranh Thế giới thứ 2."},
			{"Biến đổi khí hậu", "Phân tích tác động của biến đổi khí hậu đối với Việt Nam."},
		},
		[]float64{30, 70},
	)

	// ─── MW sheet ───
	createSheet(f, "MW - Điền chỗ trống", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Câu hỏi (dùng {} cho chỗ trống)", "Đáp án đúng", "Đáp án sai 1", "Đáp án sai 2", "Đáp án sai 3"},
		[][]string{
			{"Thủ đô Việt Nam", "Thủ đô của Việt Nam là {} nằm bên bờ sông Hồng.", "Hà Nội", "Đà Nẵng", "Huế", "Cần Thơ"},
			{"Tải Moodle", "Moodle có giá {} để tải về từ moodle.org.", "miễn phí", "rất đắt", "một ít tiền", ""},
		},
		[]float64{25, 50, 20, 20, 20, 20},
	)

	// ─── MA sheet ───
	createSheet(f, "MA - Nhiều đáp án", headerStyle, sampleStyle,
		[]string{"Tiêu đề", "Câu hỏi", "Đáp án 1", "Trọng số 1 (%)", "Đáp án 2", "Trọng số 2 (%)", "Đáp án 3", "Trọng số 3 (%)", "Đáp án 4", "Trọng số 4 (%)", "Đáp án 5", "Trọng số 5 (%)"},
		[][]string{
			{"Màu cơ bản RGB", "Đâu là các màu trong hệ RGB? (chọn tất cả đáp án đúng)", "Đỏ (Red)", "33.33333", "Xanh lá (Green)", "33.33333", "Xanh dương (Blue)", "33.33334", "Vàng", "-100", "Tím", "-100"},
			{"Lăng Grant", "Hai người nào được an táng trong lăng Grant?", "Grant", "50", "Vợ của Grant", "50", "Không ai", "-100", "Cha của Grant", "-100", "", ""},
		},
		[]float64{25, 40, 18, 12, 18, 12, 18, 12, 18, 12, 18, 12},
	)

	// ─── DESC sheet ───
	createSheet(f, "DESC - Mô tả", headerStyle, sampleStyle,
		[]string{"Nội dung mô tả"},
		[][]string{
			{"Các câu hỏi sau đây về lịch sử Việt Nam. Bạn có thể dùng giấy và bút để tính toán."},
			{"Phần 2: Câu hỏi về địa lý thế giới"},
		},
		[]float64{80},
	)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("cannot write xlsx: %w", err)
	}
	return buf, nil
}

func createSheet(f *excelize.File, name string, headerStyle, sampleStyle int, headers []string, sampleRows [][]string, widths []float64) {
	f.NewSheet(name)

	// Set column widths
	for i, w := range widths {
		col := string(rune('A' + i))
		f.SetColWidth(name, col, col, w)
	}

	// Header row
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		f.SetCellValue(name, cell, h)
		f.SetCellStyle(name, cell, cell, headerStyle)
	}

	// Sample data rows
	for rowIdx, row := range sampleRows {
		for colIdx, val := range row {
			cell := fmt.Sprintf("%s%d", string(rune('A'+colIdx)), rowIdx+2)
			f.SetCellValue(name, cell, val)
			f.SetCellStyle(name, cell, cell, sampleStyle)
		}
	}

	// Freeze header row
	f.SetPanes(name, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Sample TXT Template
// ─────────────────────────────────────────────────────────────────────────────

// GetSampleTemplateTXT returns a sample TXT template with all question types.
func GetSampleTemplateTXT() string {
	return `# ========================================================================
# MẪU IMPORT CÂU HỎI / QUESTION IMPORT TEMPLATE
# ========================================================================
# 
# HƯỚNG DẪN:
#   1. Mỗi câu hỏi bắt đầu bằng TYPE:
#   2. Các câu hỏi cách nhau bằng 1 dòng trống
#   3. Dòng bắt đầu # là ghi chú (bỏ qua khi import)
#   4. Lưu file dạng UTF-8
#   5. Upload file vào hệ thống để chuyển đổi sang GIFT
#
# CÁC LOẠI CÂU HỎI:
#   MC    - Trắc nghiệm (Multiple Choice)
#   TF    - Đúng/Sai (True/False)
#   SA    - Điền từ ngắn (Short Answer)
#   MATCH - Nối cặp (Matching) — tối thiểu 3 cặp
#   NUM   - Câu hỏi số (Numerical)
#   ESSAY - Tự luận (Essay)
#   MW    - Điền vào chỗ trống (Missing Word) — dùng {} cho chỗ trống
#   MA    - Nhiều đáp án đúng (Multiple Answers)
#   DESC  - Mô tả (Description) — không có đáp án
#
# CÁC TRƯỜNG DỮ LIỆU:
#   TYPE:     Loại câu hỏi (bắt buộc)
#   TITLE:    Tiêu đề câu hỏi (tùy chọn)
#   QUESTION: Nội dung câu hỏi (bắt buộc, trừ DESC)
#   CORRECT:  Đáp án đúng (MC, MW)
#   WRONG:    Đáp án sai (MC, MW)
#   ANSWER:   Đáp án (TF: TRUE/FALSE, SA: mỗi đáp án 1 dòng, NUM: số)
#   TOLERANCE: Sai số cho NUM
#   MIN/MAX:  Khoảng giá trị cho NUM
#   PAIR:     Cặp nối (MATCH) — định dạng: thuật ngữ -> kết quả
#   FEEDBACK: Phản hồi chung
#   CATEGORY: Phân loại/Category
# ========================================================================


# ---- 1. TRẮC NGHIỆM (Multiple Choice) ----

TYPE: MC
TITLE: Thủ đô Việt Nam
QUESTION: Thủ đô của Việt Nam là thành phố nào?
CORRECT: Hà Nội
WRONG: TP. Hồ Chí Minh
WRONG: Đà Nẵng
WRONG: Huế
FEEDBACK: Hà Nội là thủ đô của Việt Nam từ năm 1010

TYPE: MC
TITLE: Hành tinh lớn nhất
QUESTION: Hành tinh lớn nhất trong hệ Mặt Trời là gì?
CORRECT: Sao Mộc
WRONG: Sao Thổ
WRONG: Sao Hỏa
WRONG: Sao Kim


# ---- 2. ĐÚNG/SAI (True/False) ----

TYPE: TF
TITLE: Trái Đất quay quanh Mặt Trời
QUESTION: Trái Đất quay quanh Mặt Trời.
ANSWER: TRUE
FEEDBACK: Đúng! Trái Đất quay quanh Mặt Trời theo quỹ đạo elip

TYPE: TF
TITLE: Mặt Trời quay quanh Trái Đất
QUESTION: Mặt Trời quay quanh Trái Đất.
ANSWER: FALSE


# ---- 3. ĐIỀN TỪ NGẮN (Short Answer) ----

TYPE: SA
TITLE: Thủ đô Pháp
QUESTION: Thủ đô của nước Pháp là gì?
ANSWER: Paris
ANSWER: paris
ANSWER: PARIS

TYPE: SA
TITLE: Hai cộng hai
QUESTION: Hai cộng hai bằng mấy?
ANSWER: 4
ANSWER: bốn
ANSWER: Bốn


# ---- 4. NỐI CẶP (Matching) — tối thiểu 3 cặp ----

TYPE: MATCH
TITLE: Thủ đô các nước
QUESTION: Nối quốc gia với thủ đô tương ứng.
PAIR: Việt Nam -> Hà Nội
PAIR: Nhật Bản -> Tokyo
PAIR: Pháp -> Paris
PAIR: Đức -> Berlin

TYPE: MATCH
TITLE: Ngôn ngữ lập trình
QUESTION: Nối ngôn ngữ lập trình với đặc điểm chính.
PAIR: Python -> Ngôn ngữ thông dịch
PAIR: Java -> Write once run anywhere
PAIR: C -> Lập trình hệ thống


# ---- 5. CÂU HỎI SỐ (Numerical) ----

# Dạng 1: Đáp án với sai số
TYPE: NUM
TITLE: Giá trị Pi
QUESTION: Giá trị của số Pi (đến 3 chữ số thập phân)?
ANSWER: 3.14159
TOLERANCE: 0.0005

# Dạng 2: Khoảng giá trị
TYPE: NUM
TITLE: Số từ 1 đến 5
QUESTION: Cho một số nằm trong khoảng từ 1 đến 5?
MIN: 1
MAX: 5


# ---- 6. TỰ LUẬN (Essay) ----

TYPE: ESSAY
TITLE: Nguyên nhân Thế chiến II
QUESTION: Hãy trình bày nguyên nhân chính dẫn đến Chiến tranh Thế giới thứ 2.

TYPE: ESSAY
TITLE: Biến đổi khí hậu
QUESTION: Phân tích tác động của biến đổi khí hậu đối với Việt Nam.


# ---- 7. ĐIỀN VÀO CHỖ TRỐNG (Missing Word) ----
# Dùng {} để đánh dấu chỗ trống trong câu hỏi

TYPE: MW
TITLE: Thủ đô Việt Nam
QUESTION: Thủ đô của Việt Nam là {} nằm bên bờ sông Hồng.
CORRECT: Hà Nội
WRONG: Đà Nẵng
WRONG: Huế

TYPE: MW
TITLE: Tải Moodle
QUESTION: Moodle có giá {} để tải về từ moodle.org.
CORRECT: miễn phí
WRONG: rất đắt
WRONG: một ít tiền


# ---- 8. NHIỀU ĐÁP ÁN ĐÚNG (Multiple Answers) ----
# Định dạng: ANSWER: đáp án | trọng số (%)
# Tổng trọng số đáp án đúng nên = 100%
# Đáp án sai dùng trọng số âm (VD: -100)

TYPE: MA
TITLE: Màu cơ bản RGB
QUESTION: Đâu là các màu trong hệ RGB? (chọn tất cả đáp án đúng)
ANSWER: Đỏ (Red) | 33.33333
ANSWER: Xanh lá (Green) | 33.33333
ANSWER: Xanh dương (Blue) | 33.33334
ANSWER: Vàng | -100
ANSWER: Tím | -100

TYPE: MA
TITLE: Lăng Grant
QUESTION: Hai người nào được an táng trong lăng Grant?
ANSWER: Grant | 50
ANSWER: Vợ của Grant | 50
ANSWER: Không ai | -100
ANSWER: Cha của Grant | -100


# ---- 9. MÔ TẢ (Description — không có đáp án) ----

TYPE: DESC
QUESTION: Các câu hỏi sau đây về lịch sử Việt Nam. Bạn có thể dùng giấy và bút để tính toán.

TYPE: DESC
QUESTION: Phần 2: Câu hỏi về địa lý thế giới.
`
}
