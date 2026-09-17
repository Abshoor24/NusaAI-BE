package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/modules/ai/model"
)

// Repository defines database operations for conversations, messages, and RAG documents.
type Repository interface {
	CreateConversation(ctx context.Context, conv *model.Conversation) error
	GetConversation(ctx context.Context, id, userID string) (*model.Conversation, error)
	ListConversations(ctx context.Context, userID string) ([]model.Conversation, error)
	DeleteConversation(ctx context.Context, id, userID string) error
	TouchConversation(ctx context.Context, id, userID string) error

	CreateMessage(ctx context.Context, msg *model.Message) error
	GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]model.Message, error)
	GetMessages(ctx context.Context, conversationID string) ([]model.Message, error)

	// Document & RAG operations
	CreateDocument(ctx context.Context, doc *model.Document) error
	GetDocument(ctx context.Context, id, userID string) (*model.Document, error)
	ListDocuments(ctx context.Context, userID string) ([]model.DocumentDetail, error)
	DeleteDocument(ctx context.Context, id, userID string) error
	CreateDocumentChunks(ctx context.Context, chunks []model.DocumentChunk) error
	SearchSimilarChunks(ctx context.Context, embedding []float32, userID string, documentID string, limit int) ([]model.ChunkSearchResult, error)
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL-backed AI repository.
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

// --- Document & RAG operations ---

func (r *postgresRepository) CreateDocument(ctx context.Context, doc *model.Document) error {
	if r.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}

	const q = `
		INSERT INTO documents (id, user_id, title, source, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING created_at`

	err := r.pool.QueryRow(ctx, q, doc.ID, doc.UserID, doc.Title, doc.Source).Scan(&doc.CreatedAt)
	if err != nil {
		return apperror.Database("Failed to create document", err)
	}
	return nil
}

func (r *postgresRepository) GetDocument(ctx context.Context, id, userID string) (*model.Document, error) {
	if r.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}

	const q = `
		SELECT id, user_id, title, source, created_at
		FROM documents
		WHERE id = $1 AND user_id = $2`

	var d model.Document
	err := r.pool.QueryRow(ctx, q, id, userID).Scan(&d.ID, &d.UserID, &d.Title, &d.Source, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperror.Database("Failed to retrieve document", err)
	}
	return &d, nil
}

func (r *postgresRepository) ListDocuments(ctx context.Context, userID string) ([]model.DocumentDetail, error) {
	if r.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}

	const q = `
		SELECT d.id, d.user_id, d.title, d.source, d.created_at, COUNT(dc.id) AS chunk_count
		FROM documents d
		LEFT JOIN document_chunks dc ON dc.document_id = d.id
		WHERE d.user_id = $1
		GROUP BY d.id, d.user_id, d.title, d.source, d.created_at
		ORDER BY d.created_at DESC`

	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, apperror.Database("Failed to list documents", err)
	}
	defer rows.Close()

	var docs []model.DocumentDetail
	for rows.Next() {
		var dd model.DocumentDetail
		if err := rows.Scan(&dd.ID, &dd.UserID, &dd.Title, &dd.Source, &dd.CreatedAt, &dd.ChunkCount); err != nil {
			return nil, apperror.Database("Failed to read document row", err)
		}
		docs = append(docs, dd)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to list documents", err)
	}

	if docs == nil {
		docs = []model.DocumentDetail{}
	}
	return docs, nil
}

func (r *postgresRepository) DeleteDocument(ctx context.Context, id, userID string) error {
	if r.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}

	const q = `
		DELETE FROM documents
		WHERE id = $1 AND user_id = $2`

	ct, err := r.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return apperror.Database("Failed to delete document", err)
	}
	if ct.RowsAffected() == 0 {
		return apperror.NotFound("Document not found", nil)
	}
	return nil
}

func (r *postgresRepository) CreateDocumentChunks(ctx context.Context, chunks []model.DocumentChunk) error {
	if r.pool == nil {
		return apperror.Database("Database pool is not available", nil)
	}
	if len(chunks) == 0 {
		return nil
	}

	const q = `
		INSERT INTO document_chunks (id, document_id, chunk_index, content, embedding, created_at)
		VALUES ($1, $2, $3, $4, $5::vector, NOW())`

	batch := &pgx.Batch{}
	for _, chunk := range chunks {
		vecStr := formatVector(chunk.Embedding)
		batch.Queue(q, chunk.ID, chunk.DocumentID, chunk.ChunkIndex, chunk.Content, vecStr)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range chunks {
		if _, err := br.Exec(); err != nil {
			return apperror.Database("Failed to insert document chunk", err)
		}
	}
	return nil
}

func (r *postgresRepository) SearchSimilarChunks(ctx context.Context, embedding []float32, userID string, documentID string, limit int) ([]model.ChunkSearchResult, error) {
	if r.pool == nil {
		return nil, apperror.Database("Database pool is not available", nil)
	}
	if limit <= 0 {
		limit = 5
	}
	vecStr := formatVector(embedding)

	var rows pgx.Rows
	var err error
	if documentID != "" {
		const q = `
			SELECT dc.id, dc.document_id, dc.chunk_index, dc.content,
			       1 - (dc.embedding <=> $1::vector) AS similarity
			FROM document_chunks dc
			JOIN documents d ON d.id = dc.document_id
			WHERE d.user_id = $2 AND d.id = $3
			ORDER BY dc.embedding <=> $1::vector ASC
			LIMIT $4`
		rows, err = r.pool.Query(ctx, q, vecStr, userID, documentID, limit)
	} else {
		const q = `
			SELECT dc.id, dc.document_id, dc.chunk_index, dc.content,
			       1 - (dc.embedding <=> $1::vector) AS similarity
			FROM document_chunks dc
			JOIN documents d ON d.id = dc.document_id
			WHERE d.user_id = $2
			ORDER BY dc.embedding <=> $1::vector ASC
			LIMIT $3`
		rows, err = r.pool.Query(ctx, q, vecStr, userID, limit)
	}
	if err != nil {
		return nil, apperror.Database("Failed to search similar chunks", err)
	}
	defer rows.Close()

	var results []model.ChunkSearchResult
	for rows.Next() {
		var res model.ChunkSearchResult
		if err := rows.Scan(&res.ID, &res.DocumentID, &res.ChunkIndex, &res.Content, &res.Similarity); err != nil {
			return nil, apperror.Database("Failed to scan chunk search result", err)
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to search similar chunks", err)
	}

	if results == nil {
		results = []model.ChunkSearchResult{}
	}
	return results, nil
}

func formatVector(vec []float32) string {
	if len(vec) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}
