CREATE TABLE IF NOT EXISTS picks (
    id         SERIAL PRIMARY KEY,
    user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    match_id   INT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    home_score INT NOT NULL,
    away_score INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, match_id)
);
