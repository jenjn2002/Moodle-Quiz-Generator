package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/youorg/moodle-gift-generator/internal/models"
)

var DB *sql.DB

func Connect() error {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "giftuser"),
		getEnv("DB_PASSWORD", "giftpassword"),
		getEnv("DB_NAME", "giftdb"),
	)

	var err error
	for i := 0; i < 10; i++ {
		DB, err = sql.Open("postgres", dsn)
		if err == nil {
			if err = DB.Ping(); err == nil {
				log.Println("✅ Database connected")
				return nil
			}
		}
		log.Printf("⏳ Waiting for DB... attempt %d/10", i+1)
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("could not connect to database: %w", err)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func Migrate() error {
	queries := []string{
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			avatar_url TEXT DEFAULT '',
			ms_obj_id TEXT UNIQUE NOT NULL,
			password_hash TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token TEXT UNIQUE NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS question_banks (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS questions (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			bank_id UUID REFERENCES question_banks(id) ON DELETE SET NULL,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			source_text TEXT DEFAULT '',
			language TEXT DEFAULT 'vi',
			tags TEXT[] DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_questions_user_id ON questions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_questions_bank_id ON questions(bank_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT DEFAULT ''`,
	}

	for _, q := range queries {
		if _, err := DB.Exec(q); err != nil {
			return fmt.Errorf("migration error: %w\nQuery: %s", err, q)
		}
	}
	log.Println("✅ Migrations complete")
	return nil
}

// Users
func UpsertUser(u *models.User) error {
	query := `
		INSERT INTO users (ms_obj_id, email, name, avatar_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (ms_obj_id) DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = NOW()
		RETURNING id, created_at, updated_at`
	return DB.QueryRow(query, u.MSObjID, u.Email, u.Name, u.AvatarURL).
		Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func GetUserByID(id string) (*models.User, error) {
	u := &models.User{}
	err := DB.QueryRow(`SELECT id, email, name, avatar_url, ms_obj_id, created_at, updated_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.MSObjID, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Sessions
func CreateSession(userID, token string, expiresAt time.Time) error {
	_, err := DB.Exec(`INSERT INTO sessions (user_id, token, expires_at) VALUES ($1, $2, $3)`,
		userID, token, expiresAt)
	return err
}

func GetSessionUser(token string) (*models.User, error) {
	u := &models.User{}
	err := DB.QueryRow(`
		SELECT u.id, u.email, u.name, u.avatar_url, u.ms_obj_id, u.created_at, u.updated_at
		FROM sessions s JOIN users u ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > NOW()`, token).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.MSObjID, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func ExtendSession(token string, newExpiry time.Time) {
	DB.Exec(`UPDATE sessions SET expires_at=$1 WHERE token=$2`, newExpiry, token)
}

func DeleteSession(token string) error {
	_, err := DB.Exec(`DELETE FROM sessions WHERE token=$1`, token)
	return err
}

// Question Banks
func GetBanksByUser(userID string) ([]models.QuestionBank, error) {
	rows, err := DB.Query(`
		SELECT qb.id, qb.user_id, qb.title, qb.description, qb.created_at, qb.updated_at,
			COUNT(q.id) as question_count
		FROM question_banks qb
		LEFT JOIN questions q ON q.bank_id = qb.id
		WHERE qb.user_id = $1
		GROUP BY qb.id
		ORDER BY qb.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var banks []models.QuestionBank
	for rows.Next() {
		var b models.QuestionBank
		rows.Scan(&b.ID, &b.UserID, &b.Title, &b.Description, &b.CreatedAt, &b.UpdatedAt, &b.QuestionCount)
		banks = append(banks, b)
	}
	return banks, nil
}

func CreateBank(b *models.QuestionBank) error {
	return DB.QueryRow(`
		INSERT INTO question_banks (user_id, title, description)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`,
		b.UserID, b.Title, b.Description).
		Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
}

func DeleteBank(id, userID string) error {
	_, err := DB.Exec(`DELETE FROM question_banks WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// Questions
func SaveQuestions(questions []models.Question) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO questions (bank_id, user_id, type, title, content, source_text, language, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := range questions {
		q := &questions[i]
		bankID := sql.NullString{String: q.BankID, Valid: q.BankID != ""}
		err = stmt.QueryRow(bankID, q.UserID, q.Type, q.Title, q.Content, q.SourceText, q.Language, tagsToArray(q.Tags)).
			Scan(&q.ID, &q.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func GetQuestionsByUser(userID, bankID, qtype, search string) ([]models.Question, error) {
	query := `SELECT id, COALESCE(bank_id::text,''), user_id, type, title, content, source_text, language, tags, created_at, updated_at
		FROM questions WHERE user_id=$1`
	args := []interface{}{userID}
	i := 2

	if bankID != "" {
		query += fmt.Sprintf(" AND bank_id=$%d", i)
		args = append(args, bankID)
		i++
	}
	if qtype != "" {
		query += fmt.Sprintf(" AND type=$%d", i)
		args = append(args, qtype)
		i++
	}
	if search != "" {
		query += fmt.Sprintf(" AND (title ILIKE $%d OR content ILIKE $%d)", i, i)
		args = append(args, "%"+search+"%")
		i++
	}
	query += " ORDER BY created_at DESC LIMIT 200"

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var qs []models.Question
	for rows.Next() {
		var q models.Question
		var tags []byte
		rows.Scan(&q.ID, &q.BankID, &q.UserID, &q.Type, &q.Title, &q.Content, &q.SourceText, &q.Language, &tags, &q.CreatedAt, &q.UpdatedAt)
		q.Tags = parseTagArray(string(tags))
		qs = append(qs, q)
	}
	return qs, nil
}

func DeleteQuestion(id, userID string) error {
	_, err := DB.Exec(`DELETE FROM questions WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// DeleteQuestionsBulk deletes multiple questions by IDs for a given user.
func DeleteQuestionsBulk(ids []string, userID string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// Build parameterized query: DELETE FROM questions WHERE user_id=$1 AND id IN ($2,$3,...)
	args := []interface{}{userID}
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		args = append(args, id)
		placeholders[i] = fmt.Sprintf("$%d", i+2)
	}
	query := fmt.Sprintf(`DELETE FROM questions WHERE user_id=$1 AND id IN (%s)`, strings.Join(placeholders, ","))
	res, err := DB.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func tagsToArray(tags []string) string {
	if len(tags) == 0 {
		return "{}"
	}
	result := "{"
	for i, t := range tags {
		if i > 0 {
			result += ","
		}
		result += `"` + t + `"`
	}
	return result + "}"
}

func parseTagArray(s string) []string {
	if s == "{}" || s == "" {
		return []string{}
	}
	return []string{}
}

// GetUserByEmail fetches a user by email (used for local login)
func GetUserByEmail(email string) (*models.User, string, error) {
	u := &models.User{}
	var passwordHash string
	err := DB.QueryRow(`SELECT id, email, name, avatar_url, ms_obj_id, password_hash, created_at, updated_at FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.MSObjID, &passwordHash, &u.CreatedAt, &u.UpdatedAt)
	return u, passwordHash, err
}

// UpsertLocalUser creates or updates a local (non-SSO) admin account
func UpsertLocalUser(email, name, passwordHash string) (*models.User, error) {
	u := &models.User{}
	err := DB.QueryRow(`
		INSERT INTO users (ms_obj_id, email, name, avatar_url, password_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (email) DO UPDATE SET
			name = EXCLUDED.name,
			password_hash = EXCLUDED.password_hash,
			updated_at = NOW()
		RETURNING id, email, name, avatar_url, ms_obj_id, created_at, updated_at`,
		"local:"+email, email, name,
		"https://ui-avatars.com/api/?name="+name+"&background=6366f1&color=fff",
		passwordHash,
	).Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.MSObjID, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}
