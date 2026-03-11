package models

import "time"

type User struct {
	ID          string    `json:"id" db:"id"`
	Email       string    `json:"email" db:"email"`
	Name        string    `json:"name" db:"name"`
	AvatarURL   string    `json:"avatar_url" db:"avatar_url"`
	MSObjID     string    `json:"ms_obj_id" db:"ms_obj_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type QuestionBank struct {
	ID          string    `json:"id" db:"id"`
	UserID      string    `json:"user_id" db:"user_id"`
	Title       string    `json:"title" db:"title"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
	QuestionCount int     `json:"question_count" db:"question_count"`
}

type Question struct {
	ID           string    `json:"id" db:"id"`
	BankID       string    `json:"bank_id" db:"bank_id"`
	UserID       string    `json:"user_id" db:"user_id"`
	Type         string    `json:"type" db:"type"` // multiple_choice, true_false, short_answer, essay, matching
	Title        string    `json:"title" db:"title"`
	Content      string    `json:"content" db:"content"` // GIFT format
	SourceText   string    `json:"source_text" db:"source_text"`
	Language     string    `json:"language" db:"language"`
	Tags         []string  `json:"tags" db:"tags"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

type GenerateRequest struct {
	SourceText   string   `json:"source_text"`
	QuestionTypes []string `json:"question_types"`
	Count        int      `json:"count"`
	Language     string   `json:"language"`
	BankID       string   `json:"bank_id"`
	Difficulty   string   `json:"difficulty"` // easy, medium, hard
}

type GenerateResponse struct {
	Questions []Question `json:"questions"`
	GIFTText  string     `json:"gift_text"`
}

type ImportRequest struct {
	GIFTContent string `json:"gift_content"`
	BankID      string `json:"bank_id"`
	Title       string `json:"title"`
}

type Session struct {
	ID        string    `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Token     string    `json:"token" db:"token"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
