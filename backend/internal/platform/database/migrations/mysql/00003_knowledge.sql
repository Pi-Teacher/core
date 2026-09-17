-- +goose Up
-- +goose NO TRANSACTION
-- 知识内容: topic, card, trashed_card, glossary.

CREATE TABLE topic (
    id          BIGINT       NOT NULL AUTO_INCREMENT,
    name        VARCHAR(200) NOT NULL,
    description LONGTEXT     NOT NULL,
    version     BIGINT       NOT NULL DEFAULT 1,
    trashed_at  DATETIME(6)  NULL,
    created_at  DATETIME(6)  NOT NULL,
    updated_at  DATETIME(6)  NOT NULL,
    PRIMARY KEY (id),
    KEY idx_topic_name (name),
    KEY idx_topic_trashed_updated (trashed_at, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE card (
    id                BIGINT      NOT NULL AUTO_INCREMENT,
    topic_id          BIGINT      NULL,
    front             LONGTEXT    NOT NULL,
    back              LONGTEXT    NOT NULL,
    enable_embedding  BOOLEAN     NOT NULL,
    front_fingerprint BINARY(8)   NOT NULL,
    version           BIGINT      NOT NULL DEFAULT 1,
    embedding         LONGBLOB    NULL,
    embedding_status  SMALLINT    NULL,
    embedding_error   LONGTEXT    NULL,
    created_at        DATETIME(6) NOT NULL,
    updated_at        DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_card_topic_updated (topic_id, updated_at),
    KEY idx_card_created_at (created_at),
    KEY idx_card_fingerprint_enabled (front_fingerprint, enable_embedding),
    KEY idx_card_embedding_status (embedding_status, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE trashed_card (
    id               BIGINT      NOT NULL AUTO_INCREMENT,
    front            LONGTEXT    NOT NULL,
    back             LONGTEXT    NOT NULL,
    enable_embedding BOOLEAN     NOT NULL,
    version          BIGINT      NOT NULL DEFAULT 1,
    created_at       DATETIME(6) NOT NULL,
    updated_at       DATETIME(6) NOT NULL,
    trashed_at       DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_trashed_card_trashed_id (trashed_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE glossary (
    id         BIGINT       NOT NULL AUTO_INCREMENT,
    term       VARCHAR(200) NOT NULL,
    definition LONGTEXT     NOT NULL,
    version    BIGINT       NOT NULL DEFAULT 1,
    trashed_at DATETIME(6)  NULL,
    created_at DATETIME(6)  NOT NULL,
    updated_at DATETIME(6)  NOT NULL,
    PRIMARY KEY (id),
    KEY idx_glossary_term (term),
    KEY idx_glossary_trashed_updated (trashed_at, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS glossary;
DROP TABLE IF EXISTS trashed_card;
DROP TABLE IF EXISTS card;
DROP TABLE IF EXISTS topic;
