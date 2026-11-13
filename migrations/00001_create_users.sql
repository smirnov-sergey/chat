-- +goose Up

CREATE TABLE users
(
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username   TEXT COLLATE "C" NOT NULL UNIQUE,
    name       TEXT             NOT NULL,
    created_at TIMESTAMPTZ      NOT NULL,
    updated_at TIMESTAMPTZ      NOT NULL,

    CONSTRAINT users_username_format CHECK (username ~ '^[a-z0-9_]{3,32}$'),
    CONSTRAINT users_name_length CHECK (char_length(name) BETWEEN 1 AND 80)
);
