-- +goose Up
-- +goose NO TRANSACTION
-- FSRS 调度, 复习历史与每日日历计数.

CREATE TABLE card_schedule (
    card_id         BIGINT      NOT NULL,
    due             DATETIME(6) NOT NULL,
    stability       DOUBLE      NOT NULL,
    difficulty      DOUBLE      NOT NULL,
    scheduled_days  BIGINT      NOT NULL,
    reps            BIGINT      NOT NULL,
    lapses          BIGINT      NOT NULL,
    state           SMALLINT    NOT NULL,
    last_review_at  DATETIME(6) NULL,
    remaining_steps BIGINT      NOT NULL,
    version         BIGINT      NOT NULL DEFAULT 1,
    created_at      DATETIME(6) NOT NULL,
    updated_at      DATETIME(6) NOT NULL,
    PRIMARY KEY (card_id),
    KEY idx_card_schedule_due (due),
    KEY idx_card_schedule_state_due (state, due)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE review_log (
    id              BIGINT      NOT NULL AUTO_INCREMENT,
    card_id         BIGINT      NOT NULL,
    rating          SMALLINT    NOT NULL,
    due             DATETIME(6) NOT NULL,
    scheduled_days  BIGINT      NOT NULL,
    reviewed_at     DATETIME(6) NOT NULL,
    state           SMALLINT    NOT NULL,
    stability       DOUBLE      NOT NULL,
    difficulty      DOUBLE      NOT NULL,
    remaining_steps BIGINT      NOT NULL,
    created_at      DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_review_log_card_reviewed (card_id, reviewed_at, id),
    KEY idx_review_log_reviewed_at (reviewed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE calendar (
    activity_date DATE        NOT NULL,
    created_cards BIGINT      NOT NULL DEFAULT 0,
    review_events BIGINT      NOT NULL DEFAULT 0,
    created_at    DATETIME(6) NOT NULL,
    updated_at    DATETIME(6) NOT NULL,
    PRIMARY KEY (activity_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS calendar;
DROP TABLE IF EXISTS review_log;
DROP TABLE IF EXISTS card_schedule;
