-- Mobile devices (FCM tokeni) — jedan ulogovani korisnik može imati više uređaja.
-- Token je jedinstven po (user_id, device_id) ili po samom tokenu.
CREATE TABLE IF NOT EXISTS core_banking.mobile_devices (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT       NOT NULL,
    fcm_token    TEXT         NOT NULL,
    device_id    VARCHAR(128) NOT NULL DEFAULT '',
    platform     VARCHAR(16)  NOT NULL DEFAULT 'ANDROID', -- ANDROID | IOS
    last_seen_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_mobile_device_token UNIQUE (fcm_token)
);

CREATE INDEX IF NOT EXISTS idx_mobile_devices_user
    ON core_banking.mobile_devices (user_id);

-- In-app push notifications inbox.
-- Tabelu popunjavaju:
--   • Quick Approve flow (kad se kreira pending action — type='PENDING_APPROVAL')
--   • OTC notifier (counter / accepted / declined / contract expiring)
--   • Bilo koji drugi flow koji pozove fcm_dispatcher.go
CREATE TABLE IF NOT EXISTS core_banking.push_notifications (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT       NOT NULL,
    type        VARCHAR(64)  NOT NULL, -- PENDING_APPROVAL | OTC_COUNTER_OFFER | OTC_ACCEPTED | OTC_DECLINED | OTC_CONTRACT_EXPIRING
    title       VARCHAR(255) NOT NULL,
    body        TEXT         NOT NULL DEFAULT '',
    data_json   TEXT         NOT NULL DEFAULT '{}',  -- proizvoljni payload za deep link u mobilnoj app
    read_at     TIMESTAMPTZ  NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_push_notifications_user_created
    ON core_banking.push_notifications (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_push_notifications_user_unread
    ON core_banking.push_notifications (user_id) WHERE read_at IS NULL;
