CREATE TABLE IF NOT EXISTS core_banking.watchlists (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL,
    name       VARCHAR(100) NOT NULL DEFAULT 'Watchlist',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_watchlists_user ON core_banking.watchlists(user_id);

CREATE TABLE IF NOT EXISTS core_banking.watchlist_items (
    watchlist_id BIGINT NOT NULL REFERENCES core_banking.watchlists(id) ON DELETE CASCADE,
    listing_id   BIGINT NOT NULL REFERENCES core_banking.listing(id) ON DELETE CASCADE,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (watchlist_id, listing_id)
);
