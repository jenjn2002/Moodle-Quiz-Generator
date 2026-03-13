package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/youorg/moodle-gift-generator/internal/auth"
	"github.com/youorg/moodle-gift-generator/internal/db"
	"github.com/youorg/moodle-gift-generator/internal/handlers"
)

func main() {
	// Load .env if exists
	godotenv.Load()

	// Connect DB
	if err := db.Connect(); err != nil {
		log.Fatalf("DB connect: %v", err)
	}
	if err := db.Migrate(); err != nil {
		log.Fatalf("DB migrate: %v", err)
	}

	// Init auth
	auth.Init()
	auth.EnsureLocalAdmin()

	// Router
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
	}))

	// Health
	r.Get("/health", handlers.Health)

	// Auth routes
	r.Get("/login", handlers.LoginPage)
	r.Get("/auth/start", handlers.OAuthStart)
	r.Get("/auth/callback", handlers.OAuthCallback)
	r.Get("/auth/logout", handlers.Logout)
	r.Post("/auth/local", handlers.LocalLogin)

	// App (protected)
	r.With(auth.RequireAuth).Get("/app", handlers.AppPage)
	r.With(auth.RequireAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app", http.StatusTemporaryRedirect)
	})

	// API routes (protected)
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.RequireAuthAPI)

		r.Get("/me", handlers.GetMe)
		r.Post("/change-password", handlers.ChangePassword)

		// Banks
		r.Get("/banks", handlers.ListBanks)
		r.Post("/banks", handlers.CreateBank)
		r.Delete("/banks/{id}", handlers.DeleteBank)

		// Questions
		r.Get("/questions", handlers.ListQuestions)
		r.Delete("/questions/{id}", handlers.DeleteQuestion)
		r.Post("/questions/bulk-delete", handlers.BulkDeleteQuestions)

		// Generate & Import
		r.Post("/generate", handlers.Generate)
		r.Post("/import", handlers.ImportGIFT)
		r.Post("/upload", handlers.UploadFile)
		r.Post("/fetch-url", handlers.FetchURL)
		r.Get("/export", handlers.ExportGIFT)

		// Template import
		r.Get("/template/txt", handlers.DownloadTemplateTXT)
		r.Get("/template/xlsx", handlers.DownloadTemplateXLSX)
		r.Post("/import-template", handlers.ImportTemplate)
	})

	// Static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("frontend/static"))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 Server running on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
