package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
)

func (a *app) uploadImage(w http.ResponseWriter, r *http.Request, user User) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "multipart image is required")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image field is required")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	contentType := http.DetectContentType(head)
	if !strings.HasPrefix(contentType, "image/") {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusBadRequest, "failed to read image")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	if ext == "" {
		ext = ".img"
	}

	name := fmt.Sprintf("%d-%d%s", user.ID, time.Now().UnixNano(), ext)
	var publicURL string
	if env("IMAGE_STORAGE", "local") == "gcs" {
		publicURL, err = saveImageToGCS(r.Context(), file, name, contentType)
	} else {
		publicURL, err = saveImageLocal(file, name)
	}
	if err != nil {
		log.Printf("failed to save image: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save image")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"imageUrl": publicURL})
}

func saveImageLocal(file io.Reader, name string) (string, error) {
	uploadDir := env("UPLOAD_DIR", "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(uploadDir, name)
	dst, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		return "", err
	}
	publicURL := strings.TrimRight(env("PUBLIC_BASE_URL", "http://localhost:8080"), "/") + "/uploads/" + name
	return publicURL, nil
}

func saveImageToGCS(ctx context.Context, file io.Reader, name, contentType string) (string, error) {
	bucket := env("GCS_BUCKET", "")
	if bucket == "" {
		return "", errors.New("GCS_BUCKET is required")
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()

	objectName := strings.Trim(strings.TrimPrefix(env("GCS_PREFIX", "uploads"), "/"), "/") + "/" + name
	writer := client.Bucket(bucket).Object(objectName).NewWriter(ctx)
	writer.ContentType = contentType
	writer.CacheControl = "public, max-age=31536000"
	if _, err := io.Copy(writer, file); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	baseURL := strings.TrimRight(env("GCS_PUBLIC_BASE_URL", ""), "/")
	if baseURL != "" {
		return baseURL + "/" + objectName, nil
	}
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", bucket, objectName), nil
}
