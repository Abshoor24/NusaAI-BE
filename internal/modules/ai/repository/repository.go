package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
)

// Repository defines database operations for conversations and messages.
type Repository interface {
	CreateConversation(ctx context.Context, conv *model.Conversation) error
	GetConversation(ctx context.Context, id, userID string) (*model.Conversation, error)
	ListConversations(ctx context.Context, userID string) ([]model.Conversation, error)
	DeleteConversation(ctx context.Context, id, userID string) error
	TouchConversation(ctx context.Context, id, userID string) error

	CreateMessage(ctx context.Context, msg *model.Message) error
	GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]model.Message, error)
	GetMessages(ctx context.Context, conversationID string) ([]model.Message, error)
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL-backed AI conversation repository.
func New(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func (r *postgresRepository) CreateConversation(ctx context.Context, conv *model.Conversation) error {
	if r.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}

	const q = `
		INSERT INTO conversations (id, user_id, title, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING created_at, updated_at`

	err := r.pool.QueryRow(ctx, q, conv.ID, conv.UserID, conv.Title).Scan(&conv.CreatedAt, &conv.UpdatedAt)
	if err != nil {
		return apperror.Database("Failed to create conversation", err)
	}
	return nil
}

func (r *postgresRepository) GetConversation(ctx context.Context, id, userID string) (*model.Conversation, error) {
	if r.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}

	const q = `
		SELECT id, user_id, title, created_at, updated_at
		FROM conversations
		WHERE id = $1 AND user_id = $2`

	var c model.Conversation
	err := r.pool.QueryRow(ctx, q, id, userID).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperror.Database("Failed to retrieve conversation", err)
	}
	return &c, nil
}

func (r *postgresRepository) ListConversations(ctx context.Context, userID string) ([]model.Conversation, error) {
	if r.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}

	const q = `
		SELECT id, user_id, title, created_at, updated_at
		FROM conversations
		WHERE user_id = $1
		ORDER BY updated_at DESC`

	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, apperror.Database("Failed to list conversations", err)
	}
	defer rows.Close()

	var conversations []model.Conversation
	for rows.Next() {
		var c model.Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, apperror.Database("Failed to read conversation row", err)
		}
		conversations = append(conversations, c)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to list conversations", err)
	}

	if conversations == nil {
		conversations = []model.Conversation{}
	}
	return conversations, nil
}

func (r *postgresRepository) DeleteConversation(ctx context.Context, id, userID string) error {
	if r.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}

	const q = `
		DELETE FROM conversations
		WHERE id = $1 AND user_id = $2`

	ct, err := r.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return apperror.Database("Failed to delete conversation", err)
	}
	if ct.RowsAffected() == 0 {
		return apperror.NotFound("Conversation not found", nil)
	}
	return nil
}

func (r *postgresRepository) TouchConversation(ctx context.Context, id, userID string) error {
	if r.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}

	const q = `
		UPDATE conversations
		SET updated_at = NOW()
		WHERE id = $1 AND user_id = $2`

	_, err := r.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return apperror.Database("Failed to update conversation timestamp", err)
	}
	return nil
}

func (r *postgresRepository) CreateMessage(ctx context.Context, msg *model.Message) error {
	if r.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}

	const q = `
		INSERT INTO messages (id, conversation_id, role, content, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING created_at`

	err := r.pool.QueryRow(ctx, q, msg.ID, msg.ConversationID, msg.Role, msg.Content).Scan(&msg.CreatedAt)
	if err != nil {
		return apperror.Database("Failed to save message", err)
	}
	return nil
}

func (r *postgresRepository) GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]model.Message, error) {
	if r.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}

	if limit <= 0 {
		limit = 20
	}

	const q = `
		SELECT id, conversation_id, role, content, created_at
		FROM (
			SELECT id, conversation_id, role, content, created_at
			FROM messages
			WHERE conversation_id = $1
			ORDER BY created_at DESC
			LIMIT $2
		) sub
		ORDER BY created_at ASC`

	rows, err := r.pool.Query(ctx, q, conversationID, limit)
	if err != nil {
		return nil, apperror.Database("Failed to retrieve recent messages", err)
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, apperror.Database("Failed to read message row", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to retrieve recent messages", err)
	}

	if messages == nil {
		messages = []model.Message{}
	}
	return messages, nil
}

func (r *postgresRepository) GetMessages(ctx context.Context, conversationID string) ([]model.Message, error) {
	if r.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}

	const q = `
		SELECT id, conversation_id, role, content, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC`

	rows, err := r.pool.Query(ctx, q, conversationID)
	if err != nil {
		return nil, apperror.Database("Failed to retrieve conversation messages", err)
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, apperror.Database("Failed to read message row", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to retrieve conversation messages", err)
	}

	if messages == nil {
		messages = []model.Message{}
	}
	return messages, nil
}
