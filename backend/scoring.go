package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// scorePoints is a pure function — easy to unit test independently.
func scorePoints(predHome, predAway, actualHome, actualAway int) int {
	if predHome == actualHome && predAway == actualAway {
		return 3
	}
	if outcome(predHome, predAway) == outcome(actualHome, actualAway) {
		return 1
	}
	return 0
}

func outcome(home, away int) int {
	switch {
	case home > away:
		return 1
	case home < away:
		return -1
	default:
		return 0
	}
}

// recomputeMatch recalculates points for every pick on a finished match.
func recomputeMatch(ctx context.Context, db *pgxpool.Pool, matchID int) error {
	var actualHome, actualAway int
	err := db.QueryRow(ctx,
		"SELECT home_score, away_score FROM matches WHERE id=$1", matchID,
	).Scan(&actualHome, &actualAway)
	if err != nil {
		return fmt.Errorf("fetch match result: %w", err)
	}

	rows, err := db.Query(ctx,
		"SELECT id, home_score, away_score FROM picks WHERE match_id=$1", matchID,
	)
	if err != nil {
		return fmt.Errorf("fetch picks: %w", err)
	}
	defer rows.Close()

	type pickUpdate struct {
		id     int
		points int
	}
	var updates []pickUpdate
	for rows.Next() {
		var id, predHome, predAway int
		if err := rows.Scan(&id, &predHome, &predAway); err != nil {
			return fmt.Errorf("scan pick: %w", err)
		}
		updates = append(updates, pickUpdate{id, scorePoints(predHome, predAway, actualHome, actualAway)})
	}
	rows.Close()

	for _, u := range updates {
		if _, err := db.Exec(ctx,
			"UPDATE picks SET points=$1 WHERE id=$2", u.points, u.id,
		); err != nil {
			return fmt.Errorf("update pick %d: %w", u.id, err)
		}
	}

	log.Printf("recomputed scores for match %d (%d picks)", matchID, len(updates))
	return nil
}

// PATCH /api/admin/matches/{id}/result
func (s *server) handleSetResult(w http.ResponseWriter, r *http.Request) {
	matchID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		HomeScore int `json:"home_score"`
		AwayScore int `json:"away_score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.HomeScore < 0 || req.AwayScore < 0 {
		http.Error(w, "scores must be non-negative", http.StatusBadRequest)
		return
	}

	_, err = s.db.Exec(r.Context(),
		"UPDATE matches SET home_score=$1, away_score=$2 WHERE id=$3",
		req.HomeScore, req.AwayScore, matchID,
	)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("setResult: %v", err)
		return
	}

	if err := recomputeMatch(r.Context(), s.db, matchID); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("recompute: %v", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/leaderboard
type leaderboardEntry struct {
	Rank        int    `json:"rank"`
	UserID      int    `json:"user_id"`
	Email       string `json:"email"`
	TotalPoints int    `json:"total_points"`
	PicksScored int    `json:"picks_scored"`
}

func (s *server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		SELECT
			u.id,
			u.email,
			COALESCE(SUM(p.points), 0)                        AS total_points,
			COUNT(p.id) FILTER (WHERE p.points IS NOT NULL)   AS picks_scored
		FROM users u
		LEFT JOIN picks p ON p.user_id = u.id
		GROUP BY u.id, u.email
		ORDER BY total_points DESC, u.email
	`)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("leaderboard: %v", err)
		return
	}
	defer rows.Close()

	entries := make([]leaderboardEntry, 0)
	rank := 1
	for rows.Next() {
		var e leaderboardEntry
		if err := rows.Scan(&e.UserID, &e.Email, &e.TotalPoints, &e.PicksScored); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		e.Rank = rank
		rank++
		entries = append(entries, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}
