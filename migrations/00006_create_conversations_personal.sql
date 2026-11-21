-- +goose Up

ALTER TABLE conversations
    DROP CONSTRAINT conversations_type,
    ADD CONSTRAINT conversations_type CHECK (type IN ('direct', 'personal'));

CREATE TABLE conversations_personal
(
    conversation_id BIGINT PRIMARY KEY REFERENCES conversations (id),
    user_id         BIGINT NOT NULL UNIQUE REFERENCES users (id)
);

COMMENT ON TABLE conversations_personal IS 'Личные переписки («Избранное»)';
COMMENT ON COLUMN conversations_personal.user_id IS 'Владелец личной переписки';
