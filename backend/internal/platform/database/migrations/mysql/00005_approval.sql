-- +goose Up
-- +goose NO TRANSACTION
-- CLI 审批队列与幂等记录.

CREATE TABLE approval_request (
    id                      BIGINT       NOT NULL AUTO_INCREMENT,
    operation               SMALLINT     NOT NULL,
    entity_type             SMALLINT     NOT NULL,
    status                  SMALLINT     NOT NULL,
    requested_by_api_key_id BIGINT       NULL,
    original_payload        LONGTEXT     NOT NULL,
    approved_payload        LONGTEXT     NULL,
    reason                  LONGTEXT     NULL,
    created_at              DATETIME(6)  NOT NULL,
    processed_at            DATETIME(6)  NULL,
    PRIMARY KEY (id),
    KEY idx_approval_request_status_created (status, created_at, id),
    KEY idx_approval_request_api_key_created (requested_by_api_key_id, created_at),
    KEY idx_approval_request_entity_status (entity_type, operation, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE approval_target (
    id                  BIGINT      NOT NULL AUTO_INCREMENT,
    approval_request_id BIGINT      NOT NULL,
    entity_type         SMALLINT    NOT NULL,
    entity_id           BIGINT      NOT NULL,
    base_version        BIGINT      NOT NULL,
    role                VARCHAR(50) NOT NULL,
    created_at          DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_approval_target_request (approval_request_id, id),
    KEY idx_approval_target_entity (entity_type, entity_id, approval_request_id),
    UNIQUE KEY uq_approval_target_role (approval_request_id, entity_type, entity_id, role)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE idempotency_record (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    api_key_id      BIGINT       NOT NULL,
    idempotency_key VARCHAR(200) NOT NULL,
    request_method  VARCHAR(10)  NOT NULL,
    request_path    VARCHAR(500) NOT NULL,
    request_hash    VARCHAR(64)  NOT NULL,
    response_status BIGINT       NULL,
    response_body   LONGTEXT     NULL,
    state           SMALLINT     NOT NULL,
    created_at      DATETIME(6)  NOT NULL,
    expires_at      DATETIME(6)  NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_idempotency_key (api_key_id, idempotency_key),
    KEY idx_idempotency_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS idempotency_record;
DROP TABLE IF EXISTS approval_target;
DROP TABLE IF EXISTS approval_request;
