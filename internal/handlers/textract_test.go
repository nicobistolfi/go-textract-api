package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTextractHandler_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/extract", nil)
	rec := httptest.NewRecorder()

	TextractHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Success != false {
		t.Errorf("expected success to be false")
	}

	if response.Error != "Method not allowed" {
		t.Errorf("expected error 'Method not allowed', got '%s'", response.Error)
	}
}

func TestTextractHandler_MissingFiles(t *testing.T) {
	// Create a multipart form without any files
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	TextractHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Success != false {
		t.Errorf("expected success to be false")
	}

	if response.Error != "At least one file is required" {
		t.Errorf("expected error 'At least one file is required', got '%s'", response.Error)
	}
}

func TestTextractHandler_InvalidFile(t *testing.T) {
	// Create a multipart form with invalid file content
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("files", "test.pdf")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}

	// Write invalid file content
	_, _ = part.Write([]byte("This is not a valid file"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	TextractHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var response TextractResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Success != true {
		t.Errorf("expected success to be true")
	}

	if len(response.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(response.Files))
	}

	if response.Files[0].Success != false {
		t.Errorf("expected file success to be false")
	}

	if response.Files[0].Error == "" {
		t.Errorf("expected file to have an error message")
	}
}

func TestValidatePDF(t *testing.T) {
	tests := []struct {
		name        string
		pdfData     []byte
		wantErr     bool
		description string
	}{
		{
			name:        "invalid PDF data",
			pdfData:     []byte("not a pdf"),
			wantErr:     true,
			description: "should return error for invalid PDF",
		},
		{
			name:        "empty data",
			pdfData:     []byte{},
			wantErr:     true,
			description: "should return error for empty data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validatePDF(tt.pdfData)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePDF() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResponseWithError(t *testing.T) {
	rec := httptest.NewRecorder()

	respondWithError(rec, http.StatusBadRequest, "test error")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != "test error" {
		t.Errorf("expected error 'test error', got '%s'", response.Error)
	}

	if response.Success != false {
		t.Errorf("expected success to be false")
	}
}
