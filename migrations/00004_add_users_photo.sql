-- +goose Up

ALTER TABLE users
    ADD COLUMN photo TEXT,
    ADD CONSTRAINT users_photo_not_empty CHECK (photo <> '');
