package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type pickRequest struct {
	MatchID   int `json:"match_id"`
	HomeScore int `json:"home_score"`
	AwayScore int `json:"away_score"`
}

type pickRow struct {
	ID        int       `json:"id"`
	MatchID   int       `json:"match_id"`
	HomeTeam  string    `json:"home_team"`
	AwayTeam  string    `json:"away_team"`
	KickoffAt time.Time `json:"kickoff_at"`
	HomeScore int       `json:"home_score"`
	AwayScore int       `json:"away_score"`
	UpdatedAt time.Time `json:"updated_at"`
}

// POST /api/picks
func (s *server) handleCreatePick(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)

	var req pickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.HomeScore < 0 || req.AwayScore < 0 {
		http.Error(w, "scores must be non-negative", http.StatusBadRequest)
		return
	}

	if locked, err := s.isLocked(r, req.MatchID); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	} else if locked {
		http.Error(w, "match has already kicked off", http.StatusConflict)
		return
	}

	_, err := s.db.Exec(r.Context(),
		`INSERT INTO picks(user_id, match_id, home_score, away_score)
		 VALUES($1, $2, $3, $4)`,
		userID, req.MatchID, req.HomeScore, req.AwayScore,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			http.Error(w, "pick already exists — use PUT to update", http.StatusConflict)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("createPick: %v", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// PUT /api/picks/{match_id}
func (s *server) handleUpdatePick(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)

	matchID, err := strconv.Atoi(r.PathValue("match_id"))
	if err != nil {
		http.Error(w, "invalid match_id", http.StatusBadRequest)
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

	if locked, err := s.isLocked(r, matchID); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	} else if locked {
		http.Error(w, "match has already kicked off", http.StatusConflict)
		return
	}

	tag, err := s.db.Exec(r.Context(),
		`UPDATE picks SET home_score=$1, away_score=$2, updated_at=NOW()
		 WHERE user_id=$3 AND match_id=$4`,
		req.HomeScore, req.AwayScore, userID, matchID,
	)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("updatePick: %v", err)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "pick not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/picks
func (s *server) handleListPicks(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)

	rows, err := s.db.Query(r.Context(),
		`SELECT p.id, p.match_id, ht.name, at.name, m.kickoff_at,
		        p.home_score, p.away_score, p.updated_at
		 FROM picks p
		 JOIN matches m  ON m.id  = p.match_id
		 JOIN teams ht   ON ht.id = m.home_team
		 JOIN teams at   ON at.id = m.away_team
		 WHERE p.user_id = $1
		 ORDER BY m.kickoff_at`,
		userID,
	)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		log.Printf("listPicks: %v", err)
		return
	}
	defer rows.Close()

	picks := make([]pickRow, 0)
	for rows.Next() {
		var p pickRow
		if err := rows.Scan(&p.ID, &p.MatchID, &p.HomeTeam, &p.AwayTeam,
			&p.KickoffAt, &p.HomeScore, &p.AwayScore, &p.UpdatedAt); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			log.Printf("listPicks scan: %v", err)
			return
		}
		picks = append(picks, p)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(picks)
}

// isLocked returns true if the match's kickoff_at is in the past.
func (s *server) isLocked(r *http.Request, matchID int) (bool, error) {
	var kickoff time.Time
	err := s.db.QueryRow(r.Context(),
		"SELECT kickoff_at FROM matches WHERE id=$1", matchID,
	).Scan(&kickoff)
	if err != nil {
		return false, err
	}
	return time.Now().After(kickoff), nil
}
