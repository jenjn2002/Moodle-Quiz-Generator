package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/youorg/moodle-gift-generator/internal/auth"
	"github.com/youorg/moodle-gift-generator/internal/db"
	"github.com/youorg/moodle-gift-generator/internal/models"
	"github.com/youorg/moodle-gift-generator/internal/services"
)

// ============ AUTH HANDLERS ============

func LoginPage(w http.ResponseWriter, r *http.Request) {
	if _, err := auth.GetCurrentUser(r); err == nil {
		http.Redirect(w, r, "/app", http.StatusTemporaryRedirect)
		return
	}
	http.ServeFile(w, r, "frontend/templates/login.html")
}

func OAuthStart(w http.ResponseWriter, r *http.Request) {
	state, err := auth.GenerateState()
	if err != nil {
		http.Error(w, "state generation failed", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "oauth_state", Value: state,
		Path: "/", Expires: time.Now().Add(10 * time.Minute),
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	url := auth.GetOAuthConfig().AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func OAuthCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	token, err := auth.GetOAuthConfig().Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	msUser, err := auth.GetUserInfo(token)
	if err != nil {
		http.Error(w, "get user info failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := auth.CreateUserSession(w, msUser); err != nil {
		http.Error(w, "session creation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/app", http.StatusTemporaryRedirect)
}

func Logout(w http.ResponseWriter, r *http.Request) {
	auth.Logout(w, r)
	http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
}

// ============ APP PAGE ============

func AppPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "frontend/templates/app.html")
}

// ============ HELPERS ============

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

func RenderTemplate(w http.ResponseWriter, name string, data interface{}) {
	tmpl, err := template.ParseFiles("frontend/templates/" + name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tmpl.Execute(w, data)
}

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","time":"%s"}`, time.Now().Format(time.RFC3339))
}

// ============ API: USER ============

func GetMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	jsonResponse(w, 200, user)
}

// ============ API: BANKS ============

func ListBanks(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	banks, err := db.GetBanksByUser(user.ID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if banks == nil {
		banks = []models.QuestionBank{}
	}
	jsonResponse(w, 200, banks)
}

func CreateBank(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid request")
		return
	}
	if req.Title == "" {
		jsonError(w, 400, "title is required")
		return
	}
	bank := &models.QuestionBank{UserID: user.ID, Title: req.Title, Description: req.Description}
	if err := db.CreateBank(bank); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, 201, bank)
}

func DeleteBank(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	if err := db.DeleteBank(id, user.ID); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "deleted"})
}

// ============ API: QUESTIONS ============

func ListQuestions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	bankID := r.URL.Query().Get("bank_id")
	qtype := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")

	questions, err := db.GetQuestionsByUser(user.ID, bankID, qtype, search)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if questions == nil {
		questions = []models.Question{}
	}
	jsonResponse(w, 200, questions)
}

func DeleteQuestion(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	if err := db.DeleteQuestion(id, user.ID); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "deleted"})
}

// ============ API: GENERATE ============

func Generate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	var req models.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid request body")
		return
	}
	if strings.TrimSpace(req.SourceText) == "" {
		jsonError(w, 400, "source_text is required")
		return
	}

	questions, giftText, err := services.GenerateQuestions(&req, user.ID)
	if err != nil {
		jsonError(w, 500, "generation failed: "+err.Error())
		return
	}

	if len(questions) > 0 {
		if err := db.SaveQuestions(questions); err != nil {
			fmt.Printf("Warning: could not save questions: %v\n", err)
		}
	}

	jsonResponse(w, 200, models.GenerateResponse{
		Questions: questions,
		GIFTText:  giftText,
	})
}

// ============ API: IMPORT ============

func ImportGIFT(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	var req models.ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid request")
		return
	}

	questions, err := services.ParseGIFTFile(req.GIFTContent, user.ID, req.BankID)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	if err := db.SaveQuestions(questions); err != nil {
		jsonError(w, 500, "failed to save questions: "+err.Error())
		return
	}

	jsonResponse(w, 200, map[string]interface{}{
		"imported":  len(questions),
		"questions": questions,
	})
}

// ============ API: UPLOAD FILE (PDF / DOCX / TXT / GIFT) ============
// POST /api/upload
// Accepts multipart/form-data with field "file".
// Returns extracted plain text + metadata so the frontend can populate the source textarea.

func UploadFile(w http.ResponseWriter, r *http.Request) {
	// 20 MB limit — reasonable for DOCX/PDF lecture notes
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		jsonError(w, 400, "file too large (max 20 MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, 400, "no file field in request")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, 500, "could not read uploaded file")
		return
	}

	content, err := services.ExtractTextFromFile(header.Filename, data)
	if err != nil {
		jsonError(w, 422, fmt.Sprintf("could not extract text from %q: %s", header.Filename, err.Error()))
		return
	}

	info := services.BuildFileInfo(header.Filename, data, content)
	jsonResponse(w, 200, info)
}

// ============ API: EXPORT ============

func ExportGIFT(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	bankID := r.URL.Query().Get("bank_id")
	qtype := r.URL.Query().Get("type")

	questions, err := db.GetQuestionsByUser(user.ID, bankID, qtype, "")
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	var sb strings.Builder
	sb.WriteString("// GIFT Export — Generated by Moodle GIFT Generator\n")
	sb.WriteString(fmt.Sprintf("// Date: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	for _, q := range questions {
		sb.WriteString(q.Content)
		sb.WriteString("\n\n")
	}

	filename := "questions.gift"
	if bankID != "" && len(bankID) >= 8 {
		filename = fmt.Sprintf("bank_%s.gift", bankID[:8])
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write([]byte(sb.String()))
}

func init() {
	os.MkdirAll("uploads", 0755)
}

// ============ API: FETCH URL TEXT ============
// POST /api/fetch-url  {"url":"https://..."}
// Fetches a web page and extracts readable text content.

func FetchURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid request")
		return
	}
	if req.URL == "" {
		jsonError(w, 400, "url is required")
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		jsonError(w, 400, "url must start with http:// or https://")
		return
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Get(req.URL)
	if err != nil {
		jsonError(w, 502, "failed to fetch URL: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB limit
	if err != nil {
		jsonError(w, 502, "failed to read response body")
		return
	}

	// Extract text from HTML
	text := services.ExtractTextFromHTML(string(body))
	title := services.ExtractHTMLTitle(string(body))

	if text == "" {
		jsonError(w, 422, "could not extract readable text from that URL")
		return
	}

	info := services.BuildFileInfo(req.URL, body, text)
	jsonResponse(w, 200, map[string]interface{}{
		"content":    info.Content,
		"preview":    info.Preview,
		"word_count": info.WordCount,
		"title":      title,
		"url":        req.URL,
	})
}

// ============ LOCAL LOGIN ============

// POST /auth/local  (form: username, password)
func LocalLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		http.Redirect(w, r, "/login?error=empty", http.StatusSeeOther)
		return
	}
	if err := auth.LocalLogin(w, username, password); err != nil {
		http.Redirect(w, r, "/login?error=invalid", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

// POST /api/change-password  {"current_password":"...","new_password":"..."}
func ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromCtx(r.Context())
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid request")
		return
	}
	if len(req.NewPassword) < 6 {
		jsonError(w, 400, "mật khẩu mới phải ít nhất 6 ký tự")
		return
	}
	// Verify current password
	_, hash, err := db.GetUserByEmail(user.Email)
	if err != nil || !auth.CheckPassword(req.CurrentPassword, hash) {
		jsonError(w, 403, "mật khẩu hiện tại không đúng")
		return
	}
	if err := auth.ChangePassword(user.Email, req.NewPassword); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "ok"})
}
