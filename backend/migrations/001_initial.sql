CREATE TABLE IF NOT EXISTS schema_migrations (
    version INT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS teams (
    id   SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    code CHAR(3) NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS matches (
    id          SERIAL PRIMARY KEY,
    home_team   INT  NOT NULL REFERENCES teams(id),
    away_team   INT  NOT NULL REFERENCES teams(id),
    kickoff_at  TIMESTAMPTZ NOT NULL,
    home_score  INT,
    away_score  INT,
    stage       TEXT NOT NULL DEFAULT 'group',
    group_name  TEXT
);
