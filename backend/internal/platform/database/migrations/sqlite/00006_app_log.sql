-- +goose Up
-- 可选的轻量数据库日志.

CREATE TABLE app_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    logged_at   DATETIME NOT NULL,
    level       INTEGER NOT NULL,
    event       TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    request_id  TEXT    NULL,
    source      INTEGER NULL,
    entity_type INTEGER NULL,
    entity_id   INTEGER NULL,
    details     TEXT    NULL
);

CREATE INDEX idx_app_log_logged_id ON app_log (logged_at, id);
CREATE INDEX idx_app_log_level_logged ON app_log (level, logged_at);
CREATE INDEX idx_app_log_event_logged ON app_log (event, logged_at);
CREATE INDEX idx_app_log_request_id ON app_log (request_id);

-- +goose Down
DROP TABLE IF EXISTS app_log;
