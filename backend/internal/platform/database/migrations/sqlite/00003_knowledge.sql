-- +goose Up
-- 知识内容: topic, card, trashed_card, glossary.

CREATE TABLE topic (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    trashed_at  DATETIME NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

CREATE INDEX idx_topic_name ON topic (name);
CREATE INDEX idx_topic_trashed_updated ON topic (trashed_at, updated_at);

CREATE TABLE card (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    topic_id          INTEGER NULL,
    front             TEXT    NOT NULL,
    back              TEXT    NOT NULL,
    enable_embedding  INTEGER NOT NULL,
    front_fingerprint BLOB    NOT NULL,
    version           INTEGER NOT NULL DEFAULT 1,
    embedding         BLOB    NULL,
    embedding_status  INTEGER NULL,
    embedding_error   TEXT    NULL,
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);

CREATE INDEX idx_card_topic_updated ON card (topic_id, updated_at);
CREATE INDEX idx_card_created_at ON card (created_at);
CREATE INDEX idx_card_fingerprint_enabled ON card (front_fingerprint, enable_embedding);
CREATE INDEX idx_card_embedding_status ON card (embedding_status, id);

CREATE TABLE trashed_card (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    front            TEXT    NOT NULL,
    back             TEXT    NOT NULL,
    enable_embedding INTEGER NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1,
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    trashed_at       DATETIME NOT NULL
);

CREATE INDEX idx_trashed_card_trashed_id ON trashed_card (trashed_at, id);

CREATE TABLE glossary (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    term       TEXT    NOT NULL,
    definition TEXT    NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    trashed_at DATETIME NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX idx_glossary_term ON glossary (term);
CREATE INDEX idx_glossary_trashed_updated ON glossary (trashed_at, updated_at);

-- +goose Down
DROP TABLE IF EXISTS glossary;
DROP TABLE IF EXISTS trashed_card;
DROP TABLE IF EXISTS card;
DROP TABLE IF EXISTS topic;
