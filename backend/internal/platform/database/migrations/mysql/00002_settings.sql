-- +goose Up
-- +goose NO TRANSACTION
-- 实例级类型化 KV 设置.

CREATE TABLE setting_keys (
    setting_key   VARCHAR(200) NOT NULL,
    setting_value LONGTEXT     NOT NULL,
    value_type    SMALLINT     NOT NULL,
    version       BIGINT       NOT NULL DEFAULT 1,
    created_at    DATETIME(6)  NOT NULL,
    updated_at    DATETIME(6)  NOT NULL,
    PRIMARY KEY (setting_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS setting_keys;
