package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type server struct {
	db *pgxpool.Pool
	fd *fdClient // nil when FOOTBALL_DATA_API_KEY is not set
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()

	db, err := openDB(ctx, dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := runMigrations(ctx, db); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	var fd *fdClient
	if apiKey := os.Getenv("API_FOOTBALL_KEY"); apiKey != "" {
		fd = newFDClient(apiKey)
		startResultsWorker(db, fd)
		log.Println("results worker started")
	} else {
		log.Println("FOOTBALL_DATA_API_KEY not set — results worker disabled")
	}

	s := &server{db: db, fd: fd}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handleHealth)

	// auth (public)
	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	// protected
	mux.Handle("GET /api/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))
	mux.Handle("GET /api/matches", s.requireAuth(http.HandlerFunc(s.handleMatches)))
	mux.Handle("GET /api/picks", s.requireAuth(http.HandlerFunc(s.handleListPicks)))
	mux.Handle("POST /api/picks", s.requireAuth(http.HandlerFunc(s.handleCreatePick)))
	mux.Handle("PUT /api/picks/{match_id}", s.requireAuth(http.HandlerFunc(s.handleUpdatePick)))
	mux.Handle("GET /api/leaderboard", s.requireAuth(http.HandlerFunc(s.handleLeaderboard)))

	// admin (no extra auth for now — add middleware in a later phase)
	mux.HandleFunc("PATCH /api/admin/matches/{id}/result", s.handleSetResult)
	mux.HandleFunc("POST /api/admin/seed", s.handleSeed)

	log.Println("starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type matchRow struct {
	ID        int       `json:"id"`
	HomeTeam  string    `json:"home_team"`
	AwayTeam  string    `json:"away_team"`
	KickoffAt time.Time `json:"kickoff_at"`
	HomeScore *int      `json:"home_score"`
	AwayScore *int      `json:"away_score"`
	Stage     string    `json:"stage"`
	GroupName *string   `json:"group_name"`
}

func (s *server) handleMatches(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		SELECT m.id, ht.name, at.name, m.kickoff_at,
		       m.home_score, m.away_score, m.stage, m.group_name
		FROM matches m
		JOIN teams ht ON ht.id = m.home_team
		JOIN teams at ON at.id = m.away_team
		ORDER BY m.kickoff_at
	`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		log.Printf("handleMatches query: %v", err)
		return
	}
	defer rows.Close()

	matches := make([]matchRow, 0)
	for rows.Next() {
		var m matchRow
		if err := rows.Scan(&m.ID, &m.HomeTeam, &m.AwayTeam, &m.KickoffAt,
			&m.HomeScore, &m.AwayScore, &m.Stage, &m.GroupName); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			log.Printf("handleMatches scan: %v", err)
			return
		}
		matches = append(matches, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(matches)
}
