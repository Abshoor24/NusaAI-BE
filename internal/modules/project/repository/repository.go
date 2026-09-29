package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/modules/project/model"
)

// Repository is the persistence contract for projects. Only this layer
// touches the projects, project_versions, and project_attachments tables.
// Every project-scoped query is filtered by user_id so cross-user access
// yields NOT_FOUND (never FORBIDDEN), per api.md.
type Repository interface {
	Create(ctx context.Context, userID, title string, brief model.Brief) (*model.Project, error)
	List(ctx context.Context, userID string, limit, offset int) ([]*model.Project, error)
	FindByID(ctx context.Context, userID, id string) (*model.Project, error)
	FindWithLatest(ctx context.Context, userID, id string) (*model.Project, *model.Version, error)
	Delete(ctx context.Context, userID, id string) (attPaths []string, deleted bool, err error)
	// TryStartGenerate atomically transitions draft/failed -> generating.
	TryStartGenerate(ctx context.Context, userID, id string) (*model.Project, error)
	// TryStartRevise atomically transitions ready -> generating.
	TryStartRevise(ctx context.Context, userID, id string) (*model.Project, error)
	// FinishSuccess inserts the new version and flips status -> ready in one tx.
	FinishSuccess(ctx context.Context, projectID, kind, prompt, html string) (*model.Version, error)
	// FinishFailedGenerate sets status -> failed (generate failed, maybe no versions).
	FinishFailedGenerate(ctx context.Context, projectID string) error
	// FinishFailedRevise sets status -> ready (revise failed, old versions intact).
	FinishFailedRevise(ctx context.Context, projectID string) error
	// CurrentStatus returns the project status for ownership-checked rows.
	LookupStatus(ctx context.Context, userID, id string) (status string, found bool, err error)
	ListVersions(ctx context.Context, userID, projectID string) ([]*model.Version, error)
	FindVersion(ctx context.Context, userID, projectID string, number int) (*model.Version, error)
	LatestVersion(ctx context.Context, projectID string) (*model.Version, error)
	SaveAttachments(ctx context.Context, projectID string, atts []*model.ProjectAttachment) error
	ListAttachments(ctx context.Context, userID, projectID string) ([]*model.ProjectAttachment, error)
	FindAttachment(ctx context.Context, userID, projectID, attachmentID string) (*model.ProjectAttachment, string, error)
	// ResetStuckGenerating recovers projects left in generating by a crash:
	// ready if they have versions, else failed. Returns rows reset.
	ResetStuckGenerating(ctx context.Context) (int64, error)
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

func scanProject(row pgx.Row) (*model.Project, error) {
	var p model.Project
	var title string
	var status string
	var briefBytes []byte
	var versionCount int
	var created, updated time.Time
	if err := row.Scan(&p.ID, &p.UserID, &title, &status, &briefBytes, &created, &updated, &versionCount); err != nil {
		return nil, err
	}
	p.Title = title
	p.Status = status
	p.CreatedAt = created
	p.UpdatedAt = updated
	p.VersionCount = versionCount
	if len(briefBytes) > 0 {
		if err := briefJSONUnmarshal(briefBytes, &p.Brief); err != nil {
			return nil, err
		}
	}
	return &p, nil
}

const projectColumns = `p.id, p.user_id, p.title, p.status, p.brief, p.created_at, p.updated_at,
		(SELECT COUNT(*) FROM project_versions v WHERE v.project_id = p.id)`

func (r *postgresRepository) Create(ctx context.Context, userID, title string, brief model.Brief) (*model.Project, error) {
	briefBytes, err := briefJSONMarshal(brief)
	if err != nil {
		return nil, apperror.Internal("Failed to encode brief", err)
	}
	const q = `INSERT INTO projects (user_id, title, status, brief) VALUES ($1, $2, 'draft', $3)
		RETURNING id, created_at, updated_at`
	var p model.Project
	p.UserID = userID
	p.Title = title
	p.Status = model.StatusDraft
	p.Brief = brief
	if err := r.pool.QueryRow(ctx, q, userID, title, string(briefBytes)).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, apperror.Database("Failed to create project", err)
	}
	return &p, nil
}

func (r *postgresRepository) List(ctx context.Context, userID string, limit, offset int) ([]*model.Project, error) {
	const q = `SELECT ` + projectColumns + ` FROM projects p
		WHERE p.user_id = $1 ORDER BY p.updated_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, apperror.Database("Failed to list projects", err)
	}
	defer rows.Close()
	out := []*model.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, apperror.Database("Failed to read project", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to list projects", err)
	}
	return out, nil
}

func (r *postgresRepository) FindByID(ctx context.Context, userID, id string) (*model.Project, error) {
	const q = `SELECT ` + projectColumns + ` FROM projects p WHERE p.id = $1 AND p.user_id = $2`
	p, err := scanProject(r.pool.QueryRow(ctx, q, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NotFound("Project not found", nil)
	}
	if err != nil {
		return nil, apperror.Database("Failed to load project", err)
	}
	return p, nil
}

func (r *postgresRepository) FindWithLatest(ctx context.Context, userID, id string) (*model.Project, *model.Version, error) {
	p, err := r.FindByID(ctx, userID, id)
	if err != nil {
		return nil, nil, err
	}
	if p.VersionCount == 0 {
		return p, nil, nil
	}
	latest, err := r.LatestVersion(ctx, p.ID)
	if err != nil {
		return nil, nil, err
	}
	return p, latest, nil
}

func (r *postgresRepository) Delete(ctx context.Context, userID, id string) ([]string, bool, error) {
	// Ownership check first (so foreign projects -> NOT_FOUND, not CONFLICT).
	status, found, err := r.LookupStatus(ctx, userID, id)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, apperror.NotFound("Project not found", nil)
	}
	if status == model.StatusGenerating {
		return nil, false, apperror.Conflict("Cannot delete project while generation is in progress", nil)
	}
	// Collect storage paths before cascade delete removes the rows.
	const qPaths = `SELECT storage_path FROM project_attachments WHERE project_id = $1`
	rows, err := r.pool.Query(ctx, qPaths, id)
	if err != nil {
		return nil, false, apperror.Database("Failed to delete project", err)
	}
	var paths []string
	for rows.Next() {
		var sp string
		if err := rows.Scan(&sp); err != nil {
			rows.Close()
			return nil, false, apperror.Database("Failed to delete project", err)
		}
		paths = append(paths, sp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, apperror.Database("Failed to delete project", err)
	}
	const qDel = `DELETE FROM projects WHERE id = $1 AND user_id = $2`
	tag, err := r.pool.Exec(ctx, qDel, id, userID)
	if err != nil {
		return nil, false, apperror.Database("Failed to delete project", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, false, apperror.NotFound("Project not found", nil)
	}
	return paths, true, nil
}

// tryStart is the shared atomic transition helper.
func (r *postgresRepository) tryStart(ctx context.Context, userID, id string, allowed []string) (*model.Project, error) {
	const q = `UPDATE projects SET status = 'generating', updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status = ANY($3)
		RETURNING id, user_id, title, status, brief, created_at, updated_at`
	row := r.pool.QueryRow(ctx, q, id, userID, allowed)
	var p model.Project
	var briefBytes []byte
	err := row.Scan(&p.ID, &p.UserID, &p.Title, &p.Status, &briefBytes, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Distinguish NOT_FOUND (not owned) from CONFLICT (wrong state).
		status, found, lerr := r.LookupStatus(ctx, userID, id)
		if lerr != nil {
			return nil, lerr
		}
		if !found {
			return nil, apperror.NotFound("Project not found", nil)
		}
		if status == model.StatusGenerating {
			return nil, apperror.Conflict("A generation is already in progress for this project", nil)
		}
		// Caller maps the remaining case to the right message.
		return nil, &stateConflict{status: status}
	}
	if err != nil {
		return nil, apperror.Database("Failed to start generation", err)
	}
	if len(briefBytes) > 0 {
		if err := briefJSONUnmarshal(briefBytes, &p.Brief); err != nil {
			return nil, apperror.Internal("Failed to decode brief", err)
		}
	}
	// Fill version count for completeness.
	p.VersionCount = countVersions(ctx, r.pool, p.ID)
	return &p, nil
}

// stateConflict carries the current status so the service can pick the
// api.md-exact 409 message (already-has-version vs nothing-to-revise).
type stateConflict struct{ status string }

func (e *stateConflict) Error() string { return "invalid project state: " + e.status }

// AsStateConflict extracts *stateConflict from an error chain.
func AsStateConflict(err error) (string, bool) {
	var sc *stateConflict
	if errors.As(err, &sc) {
		return sc.status, true
	}
	return "", false
}

func (r *postgresRepository) TryStartGenerate(ctx context.Context, userID, id string) (*model.Project, error) {
	p, err := r.tryStart(ctx, userID, id, []string{model.StatusDraft, model.StatusFailed})
	if err != nil {
		if status, ok := AsStateConflict(err); ok {
			// generate on ready/generating: api.md message for ready case.
			_ = status
			return nil, apperror.Conflict("Project already has a version, use revise instead", nil)
		}
		return nil, err
	}
	return p, nil
}

func (r *postgresRepository) TryStartRevise(ctx context.Context, userID, id string) (*model.Project, error) {
	p, err := r.tryStart(ctx, userID, id, []string{model.StatusReady})
	if err != nil {
		if status, ok := AsStateConflict(err); ok {
			if status == model.StatusDraft || status == model.StatusFailed {
				return nil, apperror.Conflict("Project has no versions yet, use generate instead", nil)
			}
			return nil, apperror.Conflict("Project already has a version, use revise instead", nil)
		}
		return nil, err
	}
	return p, nil
}

func (r *postgresRepository) FinishSuccess(ctx context.Context, projectID, kind, prompt, html string) (*model.Version, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, apperror.Database("Failed to save version", err)
	}
	defer tx.Rollback(ctx)
	var nextNum int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM project_versions WHERE project_id = $1`, projectID).Scan(&nextNum); err != nil {
		return nil, apperror.Database("Failed to save version", err)
	}
	var v model.Version
	const qIns = `INSERT INTO project_versions (project_id, version_number, kind, prompt, html)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`
	if err := tx.QueryRow(ctx, qIns, projectID, nextNum, kind, prompt, html).Scan(&v.ID, &v.CreatedAt); err != nil {
		return nil, apperror.Database("Failed to save version", err)
	}
	v.ProjectID = projectID
	v.VersionNumber = nextNum
	v.Kind = kind
	v.Prompt = prompt
	v.HTML = html
	if _, err := tx.Exec(ctx, `UPDATE projects SET status = 'ready', updated_at = NOW() WHERE id = $1`, projectID); err != nil {
		return nil, apperror.Database("Failed to save version", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apperror.Database("Failed to save version", err)
	}
	return &v, nil
}

func (r *postgresRepository) FinishFailedGenerate(ctx context.Context, projectID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE projects SET status = 'failed', updated_at = NOW() WHERE id = $1`, projectID)
	if err != nil {
		return apperror.Database("Failed to update project status", err)
	}
	return nil
}

func (r *postgresRepository) FinishFailedRevise(ctx context.Context, projectID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE projects SET status = 'ready', updated_at = NOW() WHERE id = $1`, projectID)
	if err != nil {
		return apperror.Database("Failed to update project status", err)
	}
	return nil
}

func (r *postgresRepository) LookupStatus(ctx context.Context, userID, id string) (string, bool, error) {
	var status string
	err := r.pool.QueryRow(ctx, `SELECT status FROM projects WHERE id = $1 AND user_id = $2`, id, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, apperror.Database("Failed to load project", err)
	}
	return status, true, nil
}

func scanVersion(row pgx.Row, withHTML bool) (*model.Version, error) {
	var v model.Version
	if withHTML {
		if err := row.Scan(&v.ID, &v.ProjectID, &v.VersionNumber, &v.Kind, &v.Prompt, &v.HTML, &v.CreatedAt); err != nil {
			return nil, err
		}
	} else {
		if err := row.Scan(&v.ID, &v.ProjectID, &v.VersionNumber, &v.Kind, &v.Prompt, &v.CreatedAt); err != nil {
			return nil, err
		}
	}
	return &v, nil
}

func (r *postgresRepository) ListVersions(ctx context.Context, userID, projectID string) ([]*model.Version, error) {
	// Ownership gate first.
	if _, err := r.FindByID(ctx, userID, projectID); err != nil {
		return nil, err
	}
	const q = `SELECT id, project_id, version_number, kind, prompt, created_at
		FROM project_versions WHERE project_id = $1 ORDER BY version_number DESC`
	rows, err := r.pool.Query(ctx, q, projectID)
	if err != nil {
		return nil, apperror.Database("Failed to list versions", err)
	}
	defer rows.Close()
	out := []*model.Version{}
	for rows.Next() {
		v, err := scanVersion(rows, false)
		if err != nil {
			return nil, apperror.Database("Failed to read version", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to list versions", err)
	}
	return out, nil
}

func (r *postgresRepository) FindVersion(ctx context.Context, userID, projectID string, number int) (*model.Version, error) {
	if _, err := r.FindByID(ctx, userID, projectID); err != nil {
		return nil, err
	}
	const q = `SELECT id, project_id, version_number, kind, prompt, html, created_at
		FROM project_versions WHERE project_id = $1 AND version_number = $2`
	v, err := scanVersion(r.pool.QueryRow(ctx, q, projectID, number), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NotFound("Version not found", nil)
	}
	if err != nil {
		return nil, apperror.Database("Failed to load version", err)
	}
	return v, nil
}

func (r *postgresRepository) LatestVersion(ctx context.Context, projectID string) (*model.Version, error) {
	const q = `SELECT id, project_id, version_number, kind, prompt, html, created_at
		FROM project_versions WHERE project_id = $1 ORDER BY version_number DESC LIMIT 1`
	v, err := scanVersion(r.pool.QueryRow(ctx, q, projectID), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NotFound("Version not found", nil)
	}
	if err != nil {
		return nil, apperror.Database("Failed to load version", err)
	}
	return v, nil
}

func (r *postgresRepository) SaveAttachments(ctx context.Context, projectID string, atts []*model.ProjectAttachment) error {
	if len(atts) == 0 {
		return nil
	}
	for _, a := range atts {
		const q = `INSERT INTO project_attachments (id, project_id, file_name, file_type, file_size, storage_path, mime_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`
		if _, err := r.pool.Exec(ctx, q, a.ID, projectID, a.FileName, a.FileType, a.FileSize, a.StoragePath, a.MimeType); err != nil {
			return apperror.Database("Failed to save attachment", err)
		}
	}
	return nil
}

func (r *postgresRepository) ListAttachments(ctx context.Context, userID, projectID string) ([]*model.ProjectAttachment, error) {
	if _, err := r.FindByID(ctx, userID, projectID); err != nil {
		return nil, err
	}
	const q = `SELECT id, project_id, file_name, file_type, file_size, storage_path, mime_type, created_at
		FROM project_attachments WHERE project_id = $1 ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, projectID)
	if err != nil {
		return nil, apperror.Database("Failed to list attachments", err)
	}
	defer rows.Close()
	out := []*model.ProjectAttachment{}
	for rows.Next() {
		var a model.ProjectAttachment
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.FileName, &a.FileType, &a.FileSize, &a.StoragePath, &a.MimeType, &a.CreatedAt); err != nil {
			return nil, apperror.Database("Failed to read attachment", err)
		}
		out = append(out, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.Database("Failed to list attachments", err)
	}
	return out, nil
}

func (r *postgresRepository) FindAttachment(ctx context.Context, userID, projectID, attachmentID string) (*model.ProjectAttachment, string, error) {
	if _, err := r.FindByID(ctx, userID, projectID); err != nil {
		return nil, "", err
	}
	const q = `SELECT id, project_id, file_name, file_type, file_size, storage_path, mime_type, created_at
		FROM project_attachments WHERE id = $1 AND project_id = $2`
	var a model.ProjectAttachment
	err := r.pool.QueryRow(ctx, q, attachmentID, projectID).Scan(
		&a.ID, &a.ProjectID, &a.FileName, &a.FileType, &a.FileSize, &a.StoragePath, &a.MimeType, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", apperror.NotFound("Attachment not found", nil)
	}
	if err != nil {
		return nil, "", apperror.Database("Failed to load attachment", err)
	}
	return &a, a.StoragePath, nil
}

func (r *postgresRepository) ResetStuckGenerating(ctx context.Context) (int64, error) {
	// Projects with versions -> ready; without -> failed.
	const qReady = `UPDATE projects SET status = 'ready', updated_at = NOW() WHERE status = 'generating'
		AND EXISTS (SELECT 1 FROM project_versions v WHERE v.project_id = projects.id)`
	const qFailed = `UPDATE projects SET status = 'failed', updated_at = NOW() WHERE status = 'generating'`
	t1, err := r.pool.Exec(ctx, qReady)
	if err != nil {
		return 0, apperror.Database("Failed to recover projects", err)
	}
	t2, err := r.pool.Exec(ctx, qFailed)
	if err != nil {
		return 0, apperror.Database("Failed to recover projects", err)
	}
	return t1.RowsAffected() + t2.RowsAffected(), nil
}

func countVersions(ctx context.Context, pool *pgxpool.Pool, projectID string) int {
	var n int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_versions WHERE project_id = $1`, projectID).Scan(&n)
	return n
}
