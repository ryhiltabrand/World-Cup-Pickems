package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// api-football.com (via RapidAPI) client.
// Docs: https://www.api-football.com/documentation-v3
const afBaseURL = "https://v3.football.api-sports.io"

type fdClient struct {
	apiKey     string
	httpClient *http.Client
}

func newFDClient(apiKey string) *fdClient {
	return &fdClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// --- response shapes ---

type afFixturesResponse struct {
	Response []afFixture `json:"response"`
}

type afFixture struct {
	Fixture afFixtureInfo `json:"fixture"`
	League  afLeague      `json:"league"`
	Teams   afTeams       `json:"teams"`
	Goals   afGoals       `json:"goals"`
}

type afFixtureInfo struct {
	ID     int      `json:"id"`
	Date   string   `json:"date"`
	Status afStatus `json:"status"`
}

type afStatus struct {
	Short string `json:"short"` // NS, 1H, HT, 2H, FT, AET, PEN, PST, CANC …
}

type afLeague struct {
	Round string `json:"round"` // e.g. "Group Stage - 1", "Round of 16"
	Group string `json:"group"` // e.g. "Group A"
}

type afTeams struct {
	Home afTeamRef `json:"home"`
	Away afTeamRef `json:"away"`
}

type afTeamRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"` // 3-letter code e.g. "BRA"
}

type afGoals struct {
	Home *int `json:"home"`
	Away *int `json:"away"`
}

// isFinished returns true for any status that means the full 90+ min are done.
func (f afFixture) isFinished() bool {
	switch f.Fixture.Status.Short {
	case "FT", "AET", "PEN":
		return true
	}
	return false
}

// getFixtures fetches all fixtures for a league + season.
// leagueID 1 = FIFA World Cup.
func (c *fdClient) getFixtures(ctx context.Context, leagueID, season int) ([]afFixture, error) {
	url := fmt.Sprintf("%s/fixtures?league=%d&season=%d", afBaseURL, leagueID, season)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-apisports-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api-football returned %d", resp.StatusCode)
	}

	var out afFixturesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out.Response, nil
}
