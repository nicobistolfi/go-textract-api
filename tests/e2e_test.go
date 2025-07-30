package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicobistolfi/go-textract-api/internal/handlers"
	"github.com/nicobistolfi/go-textract-api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextractE2E(t *testing.T) {
	// Skip if AWS credentials are not configured
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		t.Skip("AWS credentials not configured, skipping E2E tests")
	}

	// Set test API key
	if err := os.Setenv("API_KEY", "test-api-key"); err != nil {
		t.Fatalf("Failed to set API_KEY: %v", err)
	}

	// Create test server with handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.HealthHandler)
	mux.HandleFunc("/extract", middleware.AuthMiddleware(handlers.TextractHandler))

	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	tests := []struct {
		name          string
		pdfFile       string
		expectedPages int
		expectSuccess bool
		checkContent  bool
	}{
		{
			name:          "Single page PDF",
			pdfFile:       "one-page.pdf",
			expectedPages: 1,
			expectSuccess: true,
			checkContent:  true,
		},
		{
			name:          "Multiple pages PDF",
			pdfFile:       "multiple-pages.pdf",
			expectedPages: 3,
			expectSuccess: true,
			checkContent:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Read test PDF file
			pdfPath := filepath.Join("data", tt.pdfFile)
			pdfData, err := os.ReadFile(pdfPath)
			require.NoError(t, err, "Failed to read test PDF file")

			// Create multipart form
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)

			// Add PDF file to form
			part, err := writer.CreateFormFile("files", tt.pdfFile)
			require.NoError(t, err, "Failed to create form file")

			_, err = part.Write(pdfData)
			require.NoError(t, err, "Failed to write PDF data")

			err = writer.Close()
			require.NoError(t, err, "Failed to close multipart writer")

			// Create request
			req, err := http.NewRequest("POST", testServer.URL+"/extract", &body)
			require.NoError(t, err, "Failed to create request")

			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("X-API-Key", "test-api-key")

			// Send request
			client := &http.Client{}
			resp, err := client.Do(req)
			require.NoError(t, err, "Failed to send request")
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			// Read response
			respBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err, "Failed to read response body")

			// Check status code
			if tt.expectSuccess {
				assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected successful response")

				// Parse response
				var result handlers.TextractResponse
				err = json.Unmarshal(respBody, &result)
				require.NoError(t, err, "Failed to parse response JSON")

				// Verify response
				assert.True(t, result.Success, "Expected success flag to be true")
				assert.Len(t, result.Files, 1, "Expected one file in response")

				fileResult := result.Files[0]
				assert.True(t, fileResult.Success, "Expected file success flag to be true")
				assert.Equal(t, tt.pdfFile, fileResult.Filename, "Filename mismatch")
				assert.Equal(t, tt.expectedPages, fileResult.TotalPages, "Unexpected number of pages")
				assert.Len(t, fileResult.Pages, tt.expectedPages, "Pages array length mismatch")

				// Check that each page has content
				if tt.checkContent {
					for i, page := range fileResult.Pages {
						assert.Equal(t, i+1, page.PageNumber, "Page number mismatch")
						assert.NotEmpty(t, page.Text, fmt.Sprintf("Page %d should have text content", i+1))
					}

					// Specific content check for multiple-pages.pdf
					if tt.pdfFile == "multiple-pages.pdf" {
						assert.Equal(t, "This is the first page", fileResult.Pages[0].Text)
						assert.Equal(t, "This is the second page", fileResult.Pages[1].Text)
						assert.Equal(t, "This is the third, and last page of the pdf.", fileResult.Pages[2].Text)
					}
				}
			} else {
				assert.NotEqual(t, http.StatusOK, resp.StatusCode, "Expected error response")

				// Parse error response
				var errorResp handlers.ErrorResponse
				err = json.Unmarshal(respBody, &errorResp)
				require.NoError(t, err, "Failed to parse error response JSON")

				assert.False(t, errorResp.Success, "Expected success flag to be false")
				assert.NotEmpty(t, errorResp.Error, "Expected error message")
			}
		})
	}
}

func TestTextractInvalidRequests(t *testing.T) {
	// Set test API key
	if err := os.Setenv("API_KEY", "test-api-key"); err != nil {
		t.Fatalf("Failed to set API_KEY: %v", err)
	}

	// Create test server with handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.HealthHandler)
	mux.HandleFunc("/extract", middleware.AuthMiddleware(handlers.TextractHandler))

	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	tests := []struct {
		name           string
		method         string
		contentType    string
		body           io.Reader
		expectedStatus int
	}{
		{
			name:           "Invalid method GET",
			method:         "GET",
			contentType:    "application/json",
			body:           nil,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "Missing PDF file",
			method:         "POST",
			contentType:    "multipart/form-data",
			body:           bytes.NewReader([]byte("invalid")),
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, testServer.URL+"/extract", tt.body)
			require.NoError(t, err, "Failed to create request")

			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			req.Header.Set("X-API-Key", "test-api-key")

			client := &http.Client{}
			resp, err := client.Do(req)
			require.NoError(t, err, "Failed to send request")
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode, "Unexpected status code")
		})
	}
}

func TestTextractMultipleFiles(t *testing.T) {
	// Skip if AWS credentials are not configured
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		t.Skip("AWS credentials not configured, skipping E2E tests")
	}

	// Set test API key
	if err := os.Setenv("API_KEY", "test-api-key"); err != nil {
		t.Fatalf("Failed to set API_KEY: %v", err)
	}

	// Create test server with handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.HealthHandler)
	mux.HandleFunc("/extract", middleware.AuthMiddleware(handlers.TextractHandler))

	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	// Read test PDF files
	pdfFiles := []string{"one-page.pdf", "multiple-pages.pdf"}

	// Create multipart form with multiple files
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	for _, pdfFile := range pdfFiles {
		pdfPath := filepath.Join("data", pdfFile)
		pdfData, err := os.ReadFile(pdfPath)
		require.NoError(t, err, "Failed to read test PDF file")

		part, err := writer.CreateFormFile("files", pdfFile)
		require.NoError(t, err, "Failed to create form file")

		_, err = part.Write(pdfData)
		require.NoError(t, err, "Failed to write PDF data")
	}

	err := writer.Close()
	require.NoError(t, err, "Failed to close multipart writer")

	// Create request
	req, err := http.NewRequest("POST", testServer.URL+"/extract", &body)
	require.NoError(t, err, "Failed to create request")

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", "test-api-key")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err, "Failed to send request")
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	// Check status code
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected successful response")

	// Parse response
	var result handlers.TextractResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err, "Failed to parse response JSON")

	// Verify response
	assert.True(t, result.Success, "Expected success flag to be true")
	assert.Len(t, result.Files, 2, "Expected two files in response")

	// Check first file (one-page.pdf)
	assert.Equal(t, "one-page.pdf", result.Files[0].Filename)
	assert.True(t, result.Files[0].Success)
	assert.Equal(t, 1, result.Files[0].TotalPages)

	// Check second file (multiple-pages.pdf)
	assert.Equal(t, "multiple-pages.pdf", result.Files[1].Filename)
	assert.True(t, result.Files[1].Success)
	assert.Equal(t, 3, result.Files[1].TotalPages)
}

func TestTextractAuthentication(t *testing.T) {
	// Set test API key
	if err := os.Setenv("API_KEY", "test-api-key"); err != nil {
		t.Fatalf("Failed to set API_KEY: %v", err)
	}

	// Create test server with handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/extract", middleware.AuthMiddleware(handlers.TextractHandler))

	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	tests := []struct {
		name           string
		apiKey         string
		expectedStatus int
	}{
		{
			name:           "Missing API key",
			apiKey:         "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Invalid API key",
			apiKey:         "wrong-key",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", testServer.URL+"/extract", nil)
			require.NoError(t, err, "Failed to create request")

			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			require.NoError(t, err, "Failed to send request")
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode, "Unexpected status code")
		})
	}
}
