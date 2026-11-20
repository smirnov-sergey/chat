-- +goose Up

CREATE TABLE conversations
(
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT conversations_type CHECK (type IN ('direct'))
);

CREATE TABLE conversations_members
(
    conversation_id BIGINT NOT NULL REFERENCES conversations (id),
    user_id         BIGINT NOT NULL REFERENCES users (id),

    PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX conversations_members_user_id_conversation_id_idx
    ON conversations_members (user_id, conversation_id);

CREATE TABLE conversations_direct
(
    conversation_id BIGINT PRIMARY KEY REFERENCES conversations (id),
    low_user_id     BIGINT NOT NULL REFERENCES users (id),
    high_user_id    BIGINT NOT NULL REFERENCES users (id),

    CONSTRAINT conversations_direct_user_order CHECK (low_user_id < high_user_id),
    CONSTRAINT conversations_direct_pair_unique UNIQUE (low_user_id, high_user_id)
);

COMMENT ON TABLE conversations IS 'Переписки';
COMMENT ON COLUMN conversations.type IS 'Тип переписки';
COMMENT ON COLUMN conversations.created_at IS 'Дата создания переписки';

COMMENT ON TABLE conversations_members IS 'Участники переписок';

COMMENT ON TABLE conversations_direct IS 'Пары участников прямых диалогов';
COMMENT ON COLUMN conversations_direct.low_user_id IS 'Меньший ID участника диалога';
COMMENT ON COLUMN conversations_direct.high_user_id IS 'Больший ID участника диалога';
