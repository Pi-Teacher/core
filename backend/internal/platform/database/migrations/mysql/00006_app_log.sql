-- +goose Up
-- +goose NO TRANSACTION
-- 可选的轻量数据库日志.

CREATE TABLE app_log (
    id          BIGINT      NOT NULL AUTO_INCREMENT,
    logged_at   DATETIME(6) NOT NULL,
    level       SMALLINT    NOT NULL,
    event       VARCHAR(100) NOT NULL,
    message     LONGTEXT    NOT NULL,
    request_id  VARCHAR(100) NULL,
    source      SMALLINT    NULL,
    entity_type SMALLINT    NULL,
    entity_id   BIGINT      NULL,
    details     LONGTEXT    NULL,
    PRIMARY KEY (id),
    KEY idx_app_log_logged_id (logged_at, id),
    KEY idx_app_log_level_logged (level, logged_at),
    KEY idx_app_log_event_logged (event, logged_at),
    KEY idx_app_log_request_id (request_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS app_log;
