-- +goose Up

ALTER TABLE conversations
    DROP CONSTRAINT conversations_type,
    ADD CONSTRAINT conversations_type CHECK (type IN ('direct', 'personal', 'group'));

CREATE TABLE conversations_group
(
    conversation_id BIGINT PRIMARY KEY REFERENCES conversations (id),
    owner_user_id   BIGINT NOT NULL,
    name            TEXT   NOT NULL,

    CONSTRAINT conversations_group_owner_member_fkey
        FOREIGN KEY (conversation_id, owner_user_id)
        REFERENCES conversations_members (conversation_id, user_id)
);

COMMENT ON TABLE conversations_group IS 'Групповые переписки';
COMMENT ON COLUMN conversations_group.owner_user_id IS 'Владелец группы';
COMMENT ON COLUMN conversations_group.name IS 'Название группы';
