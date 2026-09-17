-- +goose Up
-- 实例级类型化 KV 设置.

CREATE TABLE setting_keys (
    setting_key   VARCHAR(200) NOT NULL PRIMARY KEY,
    setting_value TEXT         NOT NULL,
    value_type    SMALLINT     NOT NULL,
    version       BIGINT       NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ  NOT NULL,
    updated_at    TIMESTAMPTZ  NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS setting_keys;
