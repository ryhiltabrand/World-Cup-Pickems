package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const ctxUserID contextKey = "userID"

// --- register ---

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Email == "" || len(req.Password) < 8 {
		http.Error(w, "email required; password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("bcrypt: %v", err)
		return
	}

	_, err = s.db.Exec(r.Context(),
		"INSERT INTO users(email, password_hash) VALUES($1, $2)",
		req.Email, string(hash),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			http.Error(w, "email already registered", http.StatusConflict)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("register insert: %v", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// --- login ---

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	var userID int
	var hash string
	err := s.db.QueryRow(r.Context(),
		"SELECT id, password_hash FROM users WHERE email=$1", req.Email,
	).Scan(&userID, &hash)
	if err != nil {
		// Same error for wrong email or wrong password — don't leak which one.
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := randomToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	expires := time.Now().Add(7 * 24 * time.Hour)
	_, err = s.db.Exec(r.Context(),
		"INSERT INTO sessions(token, user_id, expires_at) VALUES($1, $2, $3)",
		token, userID, expires,
	)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("session insert: %v", err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	w.WriteHeader(http.StatusOK)
}

// --- logout ---

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		s.db.Exec(r.Context(), "DELETE FROM sessions WHERE token=$1", cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		MaxAge: -1,
		Path:   "/",
	})
	w.WriteHeader(http.StatusOK)
}

// --- me ---

type meResponse struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)

	var me meResponse
	err := s.db.QueryRow(r.Context(),
		"SELECT id, email FROM users WHERE id=$1", userID,
	).Scan(&me.ID, &me.Email)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(me)
}

// --- middleware ---

func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var userID int
		err = s.db.QueryRow(r.Context(),
			"SELECT user_id FROM sessions WHERE token=$1 AND expires_at > NOW()",
			cookie.Value,
		).Scan(&userID)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- helpers ---

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
