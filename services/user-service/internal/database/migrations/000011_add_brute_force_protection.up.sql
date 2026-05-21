ALTER TABLE users
    ADD COLUMN failed_login_attempts INT         NOT NULL DEFAULT 0,
    ADD COLUMN account_locked_until  TIMESTAMPTZ          DEFAULT NULL;
