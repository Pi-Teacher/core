-- +goose Up
-- FSRS 调度, 复习历史与每日日历计数.

CREATE TABLE card_schedule (
    card_id         INTEGER PRIMARY KEY,
    due             DATETIME NOT NULL,
    stability       REAL    NOT NULL,
    difficulty      REAL    NOT NULL,
    scheduled_days  INTEGER NOT NULL,
    reps            INTEGER NOT NULL,
    lapses          INTEGER NOT NULL,
    state           INTEGER NOT NULL,
    last_review_at  DATETIME NULL,
    remaining_steps INTEGER NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

CREATE INDEX idx_card_schedule_due ON card_schedule (due);
CREATE INDEX idx_card_schedule_state_due ON card_schedule (state, due);

CREATE TABLE review_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    card_id         INTEGER NOT NULL,
    rating          INTEGER NOT NULL,
    due             DATETIME NOT NULL,
    scheduled_days  INTEGER NOT NULL,
    reviewed_at     DATETIME NOT NULL,
    state           INTEGER NOT NULL,
    stability       REAL    NOT NULL,
    difficulty      REAL    NOT NULL,
    remaining_steps INTEGER NOT NULL,
    created_at      DATETIME NOT NULL
);

CREATE INDEX idx_review_log_card_reviewed ON review_log (card_id, reviewed_at, id);
CREATE INDEX idx_review_log_reviewed_at ON review_log (reviewed_at);

CREATE TABLE calendar (
    activity_date DATE    NOT NULL PRIMARY KEY,
    created_cards INTEGER NOT NULL DEFAULT 0,
    review_events INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS calendar;
DROP TABLE IF EXISTS review_log;
DROP TABLE IF EXISTS card_schedule;
