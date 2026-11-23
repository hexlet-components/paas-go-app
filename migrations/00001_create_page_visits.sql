-- +goose Up
CREATE TABLE page_visits (
    path TEXT PRIMARY KEY,
    total BIGINT NOT NULL
);

-- +goose Down
DROP TABLE page_visits;
