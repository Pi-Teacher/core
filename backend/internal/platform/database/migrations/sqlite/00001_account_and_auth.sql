-- +goose Up
-- 账号, Web session 与 API Key 表.

CREATE TABLE account (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    password_hash TEXT    NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

CREATE TABLE web_session (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id      INTEGER NOT NULL,
    token_hash      TEXT    NOT NULL,
    csrf_token_hash TEXT    NOT NULL,
    created_at      DATETIME NOT NULL,
    expires_at      DATETIME NOT NULL
);

CREATE UNIQUE INDEX uq_web_session_token_hash ON web_session (token_hash);
CREATE INDEX idx_web_session_expires_at ON web_session (expires_at);
CREATE INDEX idx_web_session_account_id ON web_session (account_id);

CREATE TABLE api_key (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    name       TEXT    NOT NULL,
    api_key    TEXT    NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL
);

CREATE UNIQUE INDEX uq_api_key_api_key ON api_key (api_key);
CREATE INDEX idx_api_key_account_created ON api_key (account_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS api_key;
DROP TABLE IF EXISTS web_session;
DROP TABLE IF EXISTS account;
