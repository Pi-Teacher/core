-- +goose Up
-- CLI 审批队列与幂等记录.

CREATE TABLE approval_request (
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    operation                INTEGER NOT NULL,
    entity_type              INTEGER NOT NULL,
    status                   INTEGER NOT NULL,
    requested_by_api_key_id  INTEGER NULL,
    original_payload         TEXT    NOT NULL,
    approved_payload         TEXT    NULL,
    reason                   TEXT    NULL,
    created_at               DATETIME NOT NULL,
    processed_at             DATETIME NULL
);

CREATE INDEX idx_approval_request_status_created ON approval_request (status, created_at, id);
CREATE INDEX idx_approval_request_api_key_created ON approval_request (requested_by_api_key_id, created_at);
CREATE INDEX idx_approval_request_entity_status ON approval_request (entity_type, operation, status);

CREATE TABLE approval_target (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_request_id INTEGER NOT NULL,
    entity_type         INTEGER NOT NULL,
    entity_id           INTEGER NOT NULL,
    base_version        INTEGER NOT NULL,
    role                TEXT    NOT NULL,
    created_at          DATETIME NOT NULL
);

CREATE INDEX idx_approval_target_request ON approval_target (approval_request_id, id);
CREATE INDEX idx_approval_target_entity ON approval_target (entity_type, entity_id, approval_request_id);
CREATE UNIQUE INDEX uq_approval_target_role ON approval_target (approval_request_id, entity_type, entity_id, role);

CREATE TABLE idempotency_record (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    api_key_id      INTEGER NOT NULL,
    idempotency_key TEXT    NOT NULL,
    request_method  TEXT    NOT NULL,
    request_path    TEXT    NOT NULL,
    request_hash    TEXT    NOT NULL,
    response_status INTEGER NULL,
    response_body   TEXT    NULL,
    state           INTEGER NOT NULL,
    created_at      DATETIME NOT NULL,
    expires_at      DATETIME NOT NULL
);

CREATE UNIQUE INDEX uq_idempotency_key ON idempotency_record (api_key_id, idempotency_key);
CREATE INDEX idx_idempotency_expires_at ON idempotency_record (expires_at);

-- +goose Down
DROP TABLE IF EXISTS idempotency_record;
DROP TABLE IF EXISTS approval_target;
DROP TABLE IF EXISTS approval_request;
