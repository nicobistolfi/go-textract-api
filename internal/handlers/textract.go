package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/textract"
	"github.com/aws/aws-sdk-go-v2/service/textract/types"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	maxDocumentPages = 10
	maxFileSize      = 10 * 1024 * 1024 // 10MB
	maxWorkers       = 5
	textractTimeout  = 30 * time.Second
)

// PageResult represents the extracted text from a single page
type PageResult struct {
	PageNumber int    `json:"page_number"`
	Text       string `json:"text"`
}

// FileResult represents the result for a single file
type FileResult struct {
	Filename   string       `json:"filename"`
	Pages      []PageResult `json:"pages"`
	TotalPages int          `json:"total_pages"`
	Success    bool         `json:"success"`
	Error      string       `json:"error,omitempty"`
	index      int          // internal field for sorting
}

// TextractResponse represents the API response for multiple files
type TextractResponse struct {
	Files   []FileResult `json:"files"`
	Success bool         `json:"success"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Success bool   `json:"success"`
}

// pageJob represents a job for processing a single page
type pageJob struct {
	pageNumber int
	pageData   []byte
}

// pageWorkerResult represents the result from a worker
type pageWorkerResult struct {
	pageNumber int
	text       string
	err        error
}

// fileJob represents a job for processing a single file
type fileJob struct {
	fileHeader *multipart.FileHeader
	index      int
}

// TextractHandler handles text extraction requests for PDFs and images using AWS Textract
func TextractHandler(w http.ResponseWriter, r *http.Request) {
	slog.Info("Text extraction request started",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
		"content_type", r.Header.Get("Content-Type"))

	if r.Method != http.MethodPost {
		slog.Warn("Invalid method for text extraction",
			"method", r.Method,
			"remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse multipart form with max 10MB in memory
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		slog.Error("Failed to parse multipart form",
			"error", err,
			"remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "Failed to parse multipart form")
		return
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			slog.Warn("Failed to remove multipart form", "error", err)
		}
	}()

	// Get all files from the form
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		slog.Error("No files found in form",
			"remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "At least one file is required")
		return
	}

	slog.Info("Files received",
		"count", len(files),
		"remote_addr", r.RemoteAddr)

	// Create AWS Textract client
	ctx := context.Background()
	textractClient, err := createTextractClient(ctx)
	if err != nil {
		slog.Error("Failed to initialize AWS Textract client",
			"error", err,
			"remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusInternalServerError, "Failed to initialize AWS client")
		return
	}

	// Process files concurrently
	jobs := make(chan fileJob, len(files))
	results := make(chan FileResult, len(files))

	// Start workers
	numWorkers := maxWorkers
	if len(files) < numWorkers {
		numWorkers = len(files)
	}

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go fileWorker(ctx, textractClient, jobs, results, &wg, r.RemoteAddr)
	}

	// Send jobs
	for i, fileHeader := range files {
		jobs <- fileJob{fileHeader: fileHeader, index: i}
	}
	close(jobs)

	// Wait for workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	fileResults := make([]FileResult, len(files))
	for result := range results {
		fileResults[result.index] = result
	}

	// Sort results by original index to maintain order
	sort.Slice(fileResults, func(i, j int) bool {
		return fileResults[i].index < fileResults[j].index
	})

	// Remove index field from results
	cleanResults := make([]FileResult, len(fileResults))
	for i, result := range fileResults {
		cleanResults[i] = FileResult{
			Filename:   result.Filename,
			Pages:      result.Pages,
			TotalPages: result.TotalPages,
			Success:    result.Success,
			Error:      result.Error,
		}
	}

	// Send successful response
	response := TextractResponse{
		Files:   cleanResults,
		Success: true,
	}

	slog.Info("Text extraction completed successfully",
		"total_files", len(cleanResults),
		"remote_addr", r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode response", "error", err)
		return
	}
}

// validatePDF validates the PDF and returns the page count
func validatePDF(pdfData []byte) (int, error) {
	if len(pdfData) == 0 {
		return 0, fmt.Errorf("empty PDF data")
	}

	// Check if it starts with PDF header
	if len(pdfData) < 4 || string(pdfData[:4]) != "%PDF" {
		return 0, fmt.Errorf("invalid PDF header - not a valid PDF file")
	}

	slog.Info("Validating PDF",
		"size_bytes", len(pdfData),
		"header", string(pdfData[:min(20, len(pdfData))]))

	reader := bytes.NewReader(pdfData)

	// Validate the PDF and get page count with pdfcpu
	ctx, err := api.ReadContext(reader, nil)
	if err != nil {
		slog.Error("PDF validation failed with pdfcpu ReadContext", "error", err)
		return 0, fmt.Errorf("PDF validation failed: %w", err)
	}

	if ctx == nil {
		return 0, fmt.Errorf("PDF context is nil - corrupted PDF")
	}

	pageCount := ctx.PageCount
	slog.Info("ReadContext result",
		"page_count", pageCount,
		"has_xref_table", ctx.XRefTable != nil,
		"root_dict", ctx.RootDict != nil)

	if pageCount == 0 {
		slog.Warn("PDF reports zero pages, trying file-based approach")

		// Fallback: try with temporary file
		tmpFile, err := os.CreateTemp("", "validate-*.pdf")
		if err != nil {
			return 0, fmt.Errorf("failed to create temp file for validation: %w", err)
		}
		defer func() {
			if err := tmpFile.Close(); err != nil {
				slog.Warn("Failed to close temp file", "error", err)
			}
			if err := os.Remove(tmpFile.Name()); err != nil {
				slog.Warn("Failed to remove temp file", "error", err)
			}
		}()

		// Write PDF data to temp file
		if _, err := tmpFile.Write(pdfData); err != nil {
			return 0, fmt.Errorf("failed to write temp file: %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			slog.Warn("Failed to close temp file after write", "error", err)
		}

		// Try to get page count from file
		filePageCount, err := api.PageCountFile(tmpFile.Name())
		if err != nil {
			slog.Error("File-based page count also failed", "error", err)
			return 0, fmt.Errorf("PDF has no pages")
		}

		slog.Info("File-based validation successful", "page_count", filePageCount)
		return filePageCount, nil
	}

	slog.Info("PDF validation successful", "page_count", pageCount)
	return pageCount, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// splitPDFPages splits a PDF document into individual page PDFs
func splitPDFPages(pdfData []byte, pageCount int) ([][]byte, error) {
	pages := make([][]byte, pageCount)

	// Create a temporary directory for extraction
	tmpDir, err := os.MkdirTemp("", "pdf-extract-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.Warn("Failed to remove temp directory", "error", err, "dir", tmpDir)
		}
	}()

	// Create source PDF file in temp directory
	sourceFile := tmpDir + "/source.pdf"
	if err := os.WriteFile(sourceFile, pdfData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write source PDF: %w", err)
	}

	slog.Info("Extracting PDF pages", "source_file", sourceFile, "temp_dir", tmpDir)

	for i := 1; i <= pageCount; i++ {
		// Output file for this page
		outputFile := fmt.Sprintf("%s/page_%d.pdf", tmpDir, i)

		slog.Info("Extracting page", "page", i, "output_file", outputFile)

		// Extract single page using pdfcpu
		selectedPages := []string{fmt.Sprintf("%d", i)}
		if err := api.ExtractPagesFile(sourceFile, tmpDir, selectedPages, nil); err != nil {
			slog.Error("Failed to extract page", "page", i, "error", err)
			return nil, fmt.Errorf("failed to extract page %d: %w", i, err)
		}

		// Read the extracted page (pdfcpu creates files with pattern source_page_N.pdf)
		extractedFile := fmt.Sprintf("%s/source_page_%d.pdf", tmpDir, i)
		pageData, err := os.ReadFile(extractedFile)
		if err != nil {
			slog.Error("Failed to read extracted page", "page", i, "file", extractedFile, "error", err)
			return nil, fmt.Errorf("failed to read extracted page %d: %w", i, err)
		}

		if len(pageData) == 0 {
			return nil, fmt.Errorf("extracted page %d is empty", i)
		}

		pages[i-1] = pageData
		slog.Info("Successfully extracted page", "page", i, "size_bytes", len(pageData))
	}

	slog.Info("Successfully extracted all pages", "total_pages", pageCount)
	return pages, nil
}

// createTextractClient creates an AWS Textract client
func createTextractClient(ctx context.Context) (*textract.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	return textract.NewFromConfig(cfg), nil
}

// processPagesWithTextract processes pages concurrently using AWS Textract
func processPagesWithTextract(ctx context.Context, client *textract.Client, pages [][]byte) ([]PageResult, error) {
	numPages := len(pages)
	jobs := make(chan pageJob, numPages)
	results := make(chan pageWorkerResult, numPages)

	// Create worker pool
	var wg sync.WaitGroup
	numWorkers := maxWorkers
	if numPages < numWorkers {
		numWorkers = numPages
	}

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go textractWorker(ctx, client, jobs, results, &wg)
	}

	// Send jobs
	for i, pageData := range pages {
		jobs <- pageJob{
			pageNumber: i + 1,
			pageData:   pageData,
		}
	}
	close(jobs)

	// Wait for workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	pageResults := make([]PageResult, numPages)
	errorOccurred := false

	for result := range results {
		if result.err != nil {
			errorOccurred = true
			// Store empty result for failed pages
			pageResults[result.pageNumber-1] = PageResult{
				PageNumber: result.pageNumber,
				Text:       fmt.Sprintf("Error processing page: %v", result.err),
			}
		} else {
			pageResults[result.pageNumber-1] = PageResult{
				PageNumber: result.pageNumber,
				Text:       result.text,
			}
		}
	}

	if errorOccurred {
		return pageResults, fmt.Errorf("some pages failed to process")
	}

	return pageResults, nil
}

// textractWorker is a worker that processes pages using Textract
func textractWorker(ctx context.Context, client *textract.Client, jobs <-chan pageJob, results chan<- pageWorkerResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		text, err := extractTextWithTextract(ctx, client, job.pageData)
		results <- pageWorkerResult{
			pageNumber: job.pageNumber,
			text:       text,
			err:        err,
		}
	}
}

// extractTextWithTextract extracts text from a single page using AWS Textract
func extractTextWithTextract(ctx context.Context, client *textract.Client, pageData []byte) (string, error) {
	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, textractTimeout)
	defer cancel()

	// Call Textract DetectDocumentText API
	input := &textract.DetectDocumentTextInput{
		Document: &types.Document{
			Bytes: pageData,
		},
	}

	result, err := client.DetectDocumentText(timeoutCtx, input)
	if err != nil {
		return "", fmt.Errorf("textract API error: %w", err)
	}

	// Extract text from blocks
	var textBuilder strings.Builder
	for _, block := range result.Blocks {
		if block.BlockType == types.BlockTypeLine && block.Text != nil {
			textBuilder.WriteString(*block.Text)
			textBuilder.WriteString("\n")
		}
	}

	return strings.TrimSpace(textBuilder.String()), nil
}

// fileWorker processes individual files
func fileWorker(ctx context.Context, client *textract.Client, jobs <-chan fileJob, results chan<- FileResult, wg *sync.WaitGroup, remoteAddr string) {
	defer wg.Done()

	for job := range jobs {
		result := processFile(ctx, client, job.fileHeader, job.index, remoteAddr)
		results <- result
	}
}

// detectFileType detects whether the file is a PDF or an image based on its content
func detectFileType(data []byte) string {
	// Detect MIME type from content
	mimeType := http.DetectContentType(data)

	slog.Info("Detected MIME type", "mime_type", mimeType, "data_length", len(data))

	// Check if it's a PDF
	if strings.HasPrefix(mimeType, "application/pdf") || (len(data) >= 4 && string(data[:4]) == "%PDF") {
		return "pdf"
	}

	// Check if it's an image
	switch {
	case strings.HasPrefix(mimeType, "image/png"):
		return "image"
	case strings.HasPrefix(mimeType, "image/jpeg"):
		return "image"
	case strings.HasPrefix(mimeType, "image/jpg"):
		return "image"
	case strings.HasPrefix(mimeType, "image/tiff"):
		return "image"
	}

	return "unknown"
}

// processFile processes a single file (PDF or image)
func processFile(ctx context.Context, client *textract.Client, fileHeader *multipart.FileHeader, index int, remoteAddr string) FileResult {
	result := FileResult{
		Filename: fileHeader.Filename,
		index:    index,
		Success:  false,
	}

	// Open the file
	file, err := fileHeader.Open()
	if err != nil {
		slog.Error("Failed to open file",
			"error", err,
			"filename", fileHeader.Filename,
			"remote_addr", remoteAddr)
		result.Error = fmt.Sprintf("Failed to open file: %v", err)
		return result
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("Failed to close file", "error", err)
		}
	}()

	// Read the file
	fileData, err := io.ReadAll(io.LimitReader(file, maxFileSize))
	if err != nil {
		slog.Error("Failed to read file",
			"error", err,
			"filename", fileHeader.Filename,
			"remote_addr", remoteAddr)
		result.Error = fmt.Sprintf("Failed to read file: %v", err)
		return result
	}

	// Detect file type
	fileType := detectFileType(fileData)

	slog.Info("Processing file",
		"filename", fileHeader.Filename,
		"file_type", fileType,
		"size_bytes", len(fileData),
		"remote_addr", remoteAddr)

	switch fileType {
	case "pdf":
		// Validate PDF and get page count
		pageCount, err := validatePDF(fileData)
		if err != nil {
			slog.Error("PDF validation failed",
				"error", err,
				"filename", fileHeader.Filename,
				"remote_addr", remoteAddr)
			result.Error = fmt.Sprintf("Invalid PDF: %v", err)
			return result
		}

		if pageCount > maxDocumentPages {
			slog.Warn("PDF exceeds maximum page limit",
				"page_count", pageCount,
				"max_pages", maxDocumentPages,
				"filename", fileHeader.Filename,
				"remote_addr", remoteAddr)
			result.Error = fmt.Sprintf("Document has %d pages, maximum allowed is %d", pageCount, maxDocumentPages)
			return result
		}

		// Split PDF into individual pages
		pages, err := splitPDFPages(fileData, pageCount)
		if err != nil {
			slog.Error("Failed to split PDF pages",
				"error", err,
				"page_count", pageCount,
				"filename", fileHeader.Filename,
				"remote_addr", remoteAddr)
			result.Error = fmt.Sprintf("Failed to split PDF: %v", err)
			return result
		}

		// Process pages with Textract
		pageResults, err := processPagesWithTextract(ctx, client, pages)
		if err != nil {
			slog.Error("Failed to process PDF with Textract",
				"error", err,
				"page_count", len(pages),
				"filename", fileHeader.Filename,
				"remote_addr", remoteAddr)
			result.Error = fmt.Sprintf("Failed to process PDF: %v", err)
			return result
		}

		result.Pages = pageResults
		result.TotalPages = len(pageResults)

	case "image":
		// Process image directly with Textract (single page)
		text, err := extractTextWithTextract(ctx, client, fileData)
		if err != nil {
			slog.Error("Failed to process image with Textract",
				"error", err,
				"filename", fileHeader.Filename,
				"remote_addr", remoteAddr)
			result.Error = fmt.Sprintf("Failed to process image: %v", err)
			return result
		}

		// Create single page result for image
		result.Pages = []PageResult{
			{
				PageNumber: 1,
				Text:       text,
			},
		}
		result.TotalPages = 1

	default:
		result.Error = "Unsupported file type. Only PDF, PNG, JPG, JPEG, and TIFF files are allowed"
		return result
	}

	result.Success = true
	result.Error = ""

	slog.Info("Successfully processed file",
		"filename", fileHeader.Filename,
		"file_type", fileType,
		"total_pages", result.TotalPages,
		"remote_addr", remoteAddr)

	return result
}

// respondWithError sends an error response
func respondWithError(w http.ResponseWriter, statusCode int, message string) {
	response := ErrorResponse{
		Error:   message,
		Success: false,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Response already started, can't change status
		return
	}
}
