package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/youorg/moodle-gift-generator/internal/db"
	"github.com/youorg/moodle-gift-generator/internal/models"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const userContextKey contextKey = "user"

var oauthConfig *oauth2.Config

func Init() {
	oauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("MS365_CLIENT_ID"),
		ClientSecret: os.Getenv("MS365_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("MS365_REDIRECT_URL"),
		Scopes:       []string{"openid", "profile", "email", "User.Read"},
		Endpoint:     microsoft.AzureADEndpoint(os.Getenv("MS365_TENANT_ID")),
	}
}

func GetOAuthConfig() *oauth2.Config {
	return oauthConfig
}

func GenerateState() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

type MSUserInfo struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

func GetUserInfo(token *oauth2.Token) (*MSUserInfo, error) {
	client := oauthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://graph.microsoft.com/v1.0/me")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var info MSUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	if info.Mail == "" {
		info.Mail = info.UserPrincipalName
	}
	return &info, nil
}

func CreateUserSession(w http.ResponseWriter, msUser *MSUserInfo) error {
	user := &models.User{
		MSObjID:   msUser.ID,
		Email:     msUser.Mail,
		Name:      msUser.DisplayName,
		AvatarURL: fmt.Sprintf("https://ui-avatars.com/api/?name=%s&background=6366f1&color=fff", msUser.DisplayName),
	}
	if err := db.UpsertUser(user); err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.URLEncoding.EncodeToString(b)
	expiresAt := time.Now().Add(24 * time.Hour)
	if err := db.CreateSession(user.ID, token, expiresAt); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func GetCurrentUser(r *http.Request) (*models.User, error) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, err
	}
	return db.GetSessionUser(cookie.Value)
}

func Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		db.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:    "session",
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
	})
}

func refreshSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return
	}
	newExpiry := time.Now().Add(24 * time.Hour)
	db.ExtendSession(cookie.Value, newExpiry)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    cookie.Value,
		Path:     "/",
		Expires:  newExpiry,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := GetCurrentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}
		refreshSession(w, r)
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequireAuthAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := GetCurrentUser(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		refreshSession(w, r)
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func UserFromCtx(ctx context.Context) *models.User {
	u, _ := ctx.Value(userContextKey).(*models.User)
	return u
}

// ─── Local (username/password) auth ──────────────────────────────────────────

// HashPassword returns "sha256:<hex>".
func HashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func CheckPassword(password, hash string) bool {
	return HashPassword(password) == hash
}

// EnsureLocalAdmin creates the admin account on first boot if it doesn't exist.
func EnsureLocalAdmin() {
	email := "admin"
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		password = "admin"
	}
	hash := HashPassword(password)
	if _, err := db.UpsertLocalUser(email, "Administrator", hash); err != nil {
		fmt.Printf("WARNING: Could not ensure admin account: %v\n", err)
	} else {
		fmt.Println("Local admin account ready (username: admin)")
	}
}

// LocalLogin validates username+password and sets a session cookie.
func LocalLogin(w http.ResponseWriter, username, password string) error {
	user, hash, err := db.GetUserByEmail(username)
	if err != nil {
		return errors.New("ten dang nhap hoac mat khau khong dung")
	}
	if !strings.HasPrefix(hash, "sha256:") || !CheckPassword(password, hash) {
		return errors.New("ten dang nhap hoac mat khau khong dung")
	}
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.URLEncoding.EncodeToString(b)
	expiresAt := time.Now().Add(24 * time.Hour)
	if err := db.CreateSession(user.ID, token, expiresAt); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ChangePassword updates the local password for a given username.
func ChangePassword(username, newPassword string) error {
	hash := HashPassword(newPassword)
	_, err := db.DB.Exec(
		`UPDATE users SET password_hash=$1, updated_at=NOW() WHERE email=$2`,
		hash, username,
	)
	return err
}
