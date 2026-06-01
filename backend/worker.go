package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func startResultsWorker(db *pgxpool.Pool, fd *fdClient) {
	go func() {
		time.Sleep(10 * time.Second)
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			syncResults(db, fd)
			<-ticker.C
		}
	}()
}

func syncResults(db *pgxpool.Pool, fd *fdClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fixtures, err := fd.getFixtures(ctx, wcLeagueID, wcSeason)
	if err != nil {
		log.Printf("worker: fetch fixtures: %v", err)
		return
	}

	for _, f := range fixtures {
		if !f.isFinished() {
			continue
		}
		if f.Goals.Home == nil || f.Goals.Away == nil {
			continue
		}

		var currentHome, currentAway *int
		err := db.QueryRow(ctx,
			"SELECT home_score, away_score FROM matches WHERE id=$1", f.Fixture.ID,
		).Scan(&currentHome, &currentAway)
		if err != nil {
			continue // not seeded yet
		}

		alreadySynced := currentHome != nil && currentAway != nil &&
			*currentHome == *f.Goals.Home &&
			*currentAway == *f.Goals.Away
		if alreadySynced {
			continue
		}

		_, err = db.Exec(ctx,
			"UPDATE matches SET home_score=$1, away_score=$2 WHERE id=$3",
			f.Goals.Home, f.Goals.Away, f.Fixture.ID,
		)
		if err != nil {
			log.Printf("worker: update match %d: %v", f.Fixture.ID, err)
			continue
		}

		if err := recomputeMatch(ctx, db, f.Fixture.ID); err != nil {
			log.Printf("worker: recompute match %d: %v", f.Fixture.ID, err)
		}
	}
}
