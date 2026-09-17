-- +goose Up
-- 实例级类型化 KV 设置.

CREATE TABLE setting_keys (
    setting_key   TEXT    NOT NULL PRIMARY KEY,
    setting_value TEXT    NOT NULL,
    value_type    INTEGER NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS setting_keys;
