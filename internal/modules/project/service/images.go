package service

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/modules/project/model"
)

// supportedImageMimes mirrors the ai module's whitelist: only JPEG, PNG, WebP.
var supportedImageMimes = map[string]string{
	"image/jpeg": "jpeg",
	"image/jpg":  "jpeg",
	"image/png":  "png",
	"image/webp": "webp",
}

// normalizeImage validates one decoded image (size + MIME sniffing) in place.
func normalizeImage(fileName, mimeType string, data []byte, maxSize int64) (normMime, normName, normType string, err error) {
	if len(data) == 0 {
		return "", "", "", apperror.Validation("Image data cannot be empty")
	}
	if int64(len(data)) > maxSize {
		return "", "", "", apperror.Validation(fmt.Sprintf("Image size exceeds maximum allowed limit of %d MB", maxSize/(1<<20)))
	}
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if mime == "" || mime == "application/octet-stream" {
		switch strings.ToLower(filepath.Ext(fileName)) {
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".png":
			mime = "image/png"
		case ".webp":
			mime = "image/webp"
		default:
			if sniffed := http.DetectContentType(data); supportedImageMimes[sniffed] != "" {
				mime = sniffed
			}
		}
	}
	normType, ok := supportedImageMimes[mime]
	if !ok {
		return "", "", "", apperror.Validation("Unsupported image type. Only JPEG, PNG, and WebP are supported")
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = fmt.Sprintf("image.%s", normType)
	} else {
		fileName = filepath.Base(fileName)
	}
	return mime, fileName, normType, nil
}

// storeImages persists validated images to uploads/<userID>/<projectID>/
// and builds attachment rows. It returns provider-ready image payloads.
func storeImages(uploadDir, userID, projectID string, inputs []decodedImage) (atts []*model.ProjectAttachment, providerImgs []providerImage, err error) {
	if len(inputs) == 0 {
		return nil, nil, nil
	}
	dir := filepath.Join(uploadDir, userID, projectID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, nil, apperror.Internal("Failed to initialize storage for attachments", err)
	}
	for _, in := range inputs {
		id := uuid.New().String()
		safeName := fmt.Sprintf("%s_%s", id, in.fileName)
		target := filepath.Join(dir, safeName)
		if err := os.WriteFile(target, in.data, 0644); err != nil {
			return nil, nil, apperror.Internal("Failed to save image attachment", err)
		}
		atts = append(atts, &model.ProjectAttachment{
			ID:          id,
			ProjectID:   projectID,
			FileName:    in.fileName,
			FileType:    in.fileType,
			FileSize:    int64(len(in.data)),
			StoragePath: target,
			MimeType:    in.mimeType,
		})
		providerImgs = append(providerImgs, providerImage{MimeType: in.mimeType, Data: in.data})
	}
	return atts, providerImgs, nil
}

// decodedImage is one validated image headed for disk + the model.
type decodedImage struct {
	fileName string
	mimeType string
	fileType string
	data     []byte
}

// providerImage is the minimal image shape the service hands to the provider.
// It mirrors provider.ImageAttachment without importing the ai-owned package
// at this layer — service.go adapts it when building the provider request.
type providerImage struct {
	MimeType string
	Data     []byte
}
