-- +goose Up

ALTER TABLE users
    ADD COLUMN password_hash TEXT,
    ADD CONSTRAINT users_password_hash_not_empty CHECK (password_hash <> '');
