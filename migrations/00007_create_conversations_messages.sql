-- +goose Up

CREATE TABLE conversations_messages
(
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id BIGINT      NOT NULL REFERENCES conversations (id),
    sender_user_id  BIGINT      NOT NULL REFERENCES users (id),
    text            TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX conversations_messages_conversation_id_id_idx
    ON conversations_messages (conversation_id, id);

COMMENT ON TABLE conversations_messages IS 'Сообщения переписок';
COMMENT ON COLUMN conversations_messages.sender_user_id IS 'Отправитель сообщения';
COMMENT ON COLUMN conversations_messages.text IS 'Текст сообщения';
COMMENT ON COLUMN conversations_messages.created_at IS 'Дата создания сообщения';
