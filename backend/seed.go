package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const wcLeagueID = 1 // FIFA World Cup on api-football.com
const wcSeason = 2026

func seedMatches(ctx context.Context, db *pgxpool.Pool, fd *fdClient) error {
	fixtures, err := fd.getFixtures(ctx, wcLeagueID, wcSeason)
	if err != nil {
		return fmt.Errorf("fetch fixtures: %w", err)
	}

	inserted := 0
	for _, f := range fixtures {
		// Skip placeholder knockout fixtures where teams aren't decided yet.
		if f.Teams.Home.Code == "" || f.Teams.Away.Code == "" {
			continue
		}

		homeID, err := upsertTeam(ctx, db, f.Teams.Home.Name, f.Teams.Home.Code)
		if err != nil {
			return fmt.Errorf("upsert home team %s: %w", f.Teams.Home.Code, err)
		}
		awayID, err := upsertTeam(ctx, db, f.Teams.Away.Name, f.Teams.Away.Code)
		if err != nil {
			return fmt.Errorf("upsert away team %s: %w", f.Teams.Away.Code, err)
		}

		kickoff, err := time.Parse(time.RFC3339, f.Fixture.Date)
		if err != nil {
			return fmt.Errorf("parse date %s: %w", f.Fixture.Date, err)
		}

		_, err = db.Exec(ctx, `
			INSERT INTO matches(id, home_team, away_team, kickoff_at, stage, group_name)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO UPDATE SET
				home_team  = EXCLUDED.home_team,
				away_team  = EXCLUDED.away_team,
				kickoff_at = EXCLUDED.kickoff_at,
				stage      = EXCLUDED.stage,
				group_name = EXCLUDED.group_name
		`, f.Fixture.ID, homeID, awayID, kickoff,
			normaliseStage(f.League.Round), nullableString(f.League.Group))
		if err != nil {
			return fmt.Errorf("upsert match %d: %w", f.Fixture.ID, err)
		}
		inserted++
	}

	log.Printf("seed: upserted %d matches for WC %d", inserted, wcSeason)
	return nil
}

func upsertTeam(ctx context.Context, db *pgxpool.Pool, name, code string) (int, error) {
	var id int
	err := db.QueryRow(ctx, `
		INSERT INTO teams(name, code) VALUES($1, $2)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, name, code).Scan(&id)
	return id, err
}

func normaliseStage(round string) string {
	switch round {
	case "Group Stage - 1", "Group Stage - 2", "Group Stage - 3":
		return "group"
	case "Round of 16":
		return "r16"
	case "Quarter-finals":
		return "qf"
	case "Semi-finals":
		return "sf"
	case "3rd Place Final":
		return "3rd"
	case "Final":
		return "final"
	default:
		return round
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// POST /api/admin/seed
func (s *server) handleSeed(w http.ResponseWriter, r *http.Request) {
	if s.fd == nil {
		http.Error(w, "API_FOOTBALL_KEY not configured", http.StatusServiceUnavailable)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := seedMatches(ctx, s.db, s.fd); err != nil {
			log.Printf("seed error: %v", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}
