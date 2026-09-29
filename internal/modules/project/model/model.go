// Package model defines the request/response DTOs for the AI Website
// Generator (Tahap A): projects, briefs, versions, and attachments.
package model

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Project statuses (state machine).
const (
	StatusDraft      = "draft"
	StatusGenerating = "generating"
	StatusReady      = "ready"
	StatusFailed     = "failed"
)

// Version kinds.
const (
	KindGenerate = "generate"
	KindRevise   = "revise"
)

// Website types and languages (whitelists from api.md).
var ValidWebsiteTypes = map[string]bool{
	"landing_page": true, "portfolio": true, "company_profile": true,
	"online_store": true, "blog": true, "other": true,
}

var ValidLanguages = map[string]bool{"id": true, "en": true}

// Brief is the structured website description stored as JSONB.
type Brief struct {
	Description  string   `json:"description" validate:"required,min=10,max=4000"`
	WebsiteType  string   `json:"website_type,omitempty" validate:"omitempty,oneof=landing_page portfolio company_profile online_store blog other"`
	BusinessName string   `json:"business_name,omitempty" validate:"omitempty,max=120"`
	Style        string   `json:"style,omitempty" validate:"omitempty,max=200"`
	ColorPalette string   `json:"color_palette,omitempty" validate:"omitempty,max=200"`
	Language     string   `json:"language,omitempty" validate:"omitempty,oneof=id en"`
	Sections     []string `json:"sections,omitempty" validate:"omitempty,max=12,dive,max=50"`
}

// ApplyDefaults fills website_type (landing_page) and language (id).
func (b *Brief) ApplyDefaults() {
	if strings.TrimSpace(b.WebsiteType) == "" {
		b.WebsiteType = "landing_page"
	}
	if strings.TrimSpace(b.Language) == "" {
		b.Language = "id"
	}
}

// DeriveTitle implements the api.md title fallback:
// title -> business_name -> first 60 chars of description.
func DeriveTitle(title string, b Brief) string {
	if s := strings.TrimSpace(title); s != "" {
		return s
	}
	if s := strings.TrimSpace(b.BusinessName); s != "" {
		return s
	}
	desc := strings.TrimSpace(b.Description)
	if utf8.RuneCountInString(desc) <= 60 {
		return desc
	}
	runes := []rune(desc)
	return string(runes[:60])
}

// Project is the stored project row.
type Project struct {
	ID           string    `json:"id"`
	UserID       string    `json:"-"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	Brief        Brief     `json:"brief"`
	VersionCount int       `json:"version_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Version is one stored HTML generation.
type Version struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	VersionNumber int       `json:"version_number"`
	Kind          string    `json:"kind"`
	Prompt        string    `json:"prompt"`
	HTML          string    `json:"html,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ProjectAttachment is a reference image owned by a project.
type ProjectAttachment struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	FileName    string    `json:"file_name"`
	FileType    string    `json:"file_type"`
	FileSize    int64     `json:"file_size"`
	StoragePath string    `json:"-"`
	MimeType    string    `json:"mime_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectResponse is the api.md ProjectResponse shape.
// LatestVersion is only populated on GET /projects/{id} (null when the
// project has no versions yet); list items leave it nil.
type ProjectResponse struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	Status        string           `json:"status"`
	Brief         Brief            `json:"brief"`
	VersionCount  int              `json:"version_count"`
	LatestVersion *VersionResponse `json:"latest_version"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// VersionResponse is the api.md VersionResponse shape.
type VersionResponse struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	VersionNumber int       `json:"version_number"`
	Kind          string    `json:"kind"`
	Prompt        string    `json:"prompt"`
	HTML          string    `json:"html,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// CreateProjectRequest is POST /projects body.
type CreateProjectRequest struct {
	Title string `json:"title,omitempty" validate:"omitempty,max=200"`
	Brief Brief  `json:"brief" validate:"required"`
}

// ImagePayload is one base64 image in JSON generate/revise bodies.
type ImagePayload struct {
	MimeType string `json:"mime_type" validate:"required"`
	Data     string `json:"data" validate:"required"`
	FileName string `json:"file_name,omitempty" validate:"omitempty,max=255"`
}

// GenerateRequest is POST /projects/{id}/generate JSON body.
// The image count cap is enforced in the service from PROJECT_MAX_IMAGES
// (not here) so operators can raise the limit without a code change.
type GenerateRequest struct {
	Images []ImagePayload `json:"images,omitempty" validate:"omitempty,dive"`
}

// ReviseRequest is POST /projects/{id}/revise JSON body.
type ReviseRequest struct {
	Instruction string         `json:"instruction" validate:"required,min=1,max=4000"`
	BaseVersion *int           `json:"base_version,omitempty" validate:"omitempty,min=1"`
	Images      []ImagePayload `json:"images,omitempty" validate:"omitempty,dive"`
}

// ToProjectResponse converts a Project (+ optional latest version).
func ToProjectResponse(p *Project, latest *Version) ProjectResponse {
	resp := ProjectResponse{
		ID:           p.ID,
		Title:        p.Title,
		Status:       p.Status,
		Brief:        p.Brief,
		VersionCount: p.VersionCount,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
	if latest != nil {
		v := ToVersionResponse(latest, true)
		resp.LatestVersion = &v
	}
	return resp
}

// ToVersionResponse converts a Version; includeHTML controls the html field.
func ToVersionResponse(v *Version, includeHTML bool) VersionResponse {
	out := VersionResponse{
		ID:            v.ID,
		ProjectID:     v.ProjectID,
		VersionNumber: v.VersionNumber,
		Kind:          v.Kind,
		Prompt:        v.Prompt,
		CreatedAt:     v.CreatedAt,
	}
	if includeHTML {
		out.HTML = v.HTML
	}
	return out
}

// ToAttachmentResponse converts storage fields to the api.md shape.
func ToAttachmentResponse(a *ProjectAttachment) ProjectAttachment {
	return *a
}
