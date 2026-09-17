-- +goose Up
-- +goose NO TRANSACTION
-- 账号, Web session 与 API Key 表.

CREATE TABLE account (
    id            BIGINT       NOT NULL AUTO_INCREMENT,
    password_hash LONGTEXT     NOT NULL,
    version       BIGINT       NOT NULL DEFAULT 1,
    created_at    DATETIME(6)  NOT NULL,
    updated_at    DATETIME(6)  NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE web_session (
    id              BIGINT      NOT NULL AUTO_INCREMENT,
    account_id      BIGINT      NOT NULL,
    token_hash      VARCHAR(64) NOT NULL,
    csrf_token_hash VARCHAR(64) NOT NULL,
    created_at      DATETIME(6) NOT NULL,
    expires_at      DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_web_session_token_hash (token_hash),
    KEY idx_web_session_expires_at (expires_at),
    KEY idx_web_session_account_id (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE api_key (
    id         BIGINT       NOT NULL AUTO_INCREMENT,
    account_id BIGINT       NOT NULL,
    name       VARCHAR(200) NOT NULL,
    api_key    VARCHAR(128) NOT NULL,
    version    BIGINT       NOT NULL DEFAULT 1,
    created_at DATETIME(6)  NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_api_key_api_key (api_key),
    KEY idx_api_key_account_created (account_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS api_key;
DROP TABLE IF EXISTS web_session;
DROP TABLE IF EXISTS account;
