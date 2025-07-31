// Go Textract API
//
// A Golang HTTP API for extracting text from documents and images using AWS Textract.
// Features API key authentication and designed for serverless deployment on AWS Lambda.
//
// Supported file formats:
// - PDF documents (multi-page, up to 10 pages per file)
// - Images: PNG, JPEG/JPG, TIFF (single page)
//
// The API uses MIME type detection for accurate file type identification and supports
// concurrent processing of multiple files in a single request.
//
//	@title						Go Textract API
//	@version					1.0
//	@description				A Golang HTTP API for extracting text from documents and images using AWS Textract, featuring API key authentication and designed for serverless deployment on AWS Lambda.
//	@termsOfService				http://swagger.io/terms/
//	@contact.name				API Support
//	@contact.url				https://github.com/nicobistolfi/go-textract-api
//	@license.name				MIT
//	@license.url				https://opensource.org/licenses/MIT
//	@host						localhost:8080
//	@BasePath					/
//	@schemes					http https
//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						X-API-Key
//	@description				API key authentication via X-API-Key header
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/nicobistolfi/go-textract-api/internal/handlers"
	"github.com/nicobistolfi/go-textract-api/internal/logging"
	_ "github.com/nicobistolfi/go-textract-api/internal/models"

	_ "github.com/nicobistolfi/go-textract-api/docs"
)

// @title						Go Lambda API
// @version					1.0
// @description				A serverless API built with AWS Lambda and Go
// @host						localhost:8080
// @BasePath					/
// @securityDefinitions.apikey	ApiKeyAuth
// @in							header
// @name						X-API-Key
func handleRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	slog.Info("Lambda request received",
		"method", request.RequestContext.HTTP.Method,
		"path", request.RawPath,
		"source_ip", request.RequestContext.HTTP.SourceIP,
		"user_agent", request.RequestContext.HTTP.UserAgent)

	//	@Summary		Health check endpoint
	//	@Description	Check if the API is running
	//	@Tags			health
	//	@Accept			json
	//	@Produce		json
	//	@Success		200	{object}	handlers.HealthResponse
	//	@Router			/health [get]
	// Handle health endpoint
	if request.RawPath == "/health" {
		slog.Info("Processing health check request")
		response := handlers.HealthResponse{
			Status: "ok",
		}

		body, err := json.Marshal(response)
		if err != nil {
			slog.Error("Failed to marshal health response", "error", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":"Internal server error"}`,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			}, nil
		}

		slog.Info("Health check completed successfully")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusOK,
			Body:       string(body),
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	// For other endpoints, check authentication first
	apiKey, exists := request.Headers["x-api-key"]
	if !exists {
		slog.Warn("Missing API key in request",
			"path", request.RawPath,
			"source_ip", request.RequestContext.HTTP.SourceIP)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       `{"error":"Missing API key"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	// Validate API key
	expectedAPIKey := os.Getenv("API_KEY")
	if expectedAPIKey == "" {
		slog.Error("API key not configured in environment")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       `{"error":"API key not configured"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	if apiKey != expectedAPIKey {
		slog.Warn("Invalid API key provided",
			"path", request.RawPath,
			"source_ip", request.RequestContext.HTTP.SourceIP)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       `{"error":"Invalid API key"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	// Handle PDF extraction endpoint
	if request.RawPath == "/extract" && request.RequestContext.HTTP.Method == "POST" {
		slog.Info("Processing PDF extraction request")

		// Convert Lambda request to HTTP request and process
		response, err := handlePDFExtraction(ctx, request)
		if err != nil {
			slog.Error("Failed to handle PDF extraction", "error", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":"Internal server error"}`,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			}, nil
		}

		return response, nil
	}

	// Handle other routes here in the future
	slog.Warn("Route not found",
		"path", request.RawPath,
		"method", request.RequestContext.HTTP.Method,
		"source_ip", request.RequestContext.HTTP.SourceIP)

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"Not found"}`,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}, nil
}

// handlePDFExtraction converts Lambda request to HTTP request and processes PDF extraction
func handlePDFExtraction(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	// Decode the body if it's base64 encoded
	var bodyBytes []byte
	var err error

	if request.IsBase64Encoded {
		bodyBytes, err = base64.StdEncoding.DecodeString(request.Body)
		if err != nil {
			slog.Error("Failed to decode base64 body", "error", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       `{"error":"Invalid request body encoding"}`,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			}, nil
		}
	} else {
		bodyBytes = []byte(request.Body)
	}

	// Create HTTP request
	httpReq := httptest.NewRequest("POST", "/extract", bytes.NewReader(bodyBytes))

	// Set headers
	for key, value := range request.Headers {
		httpReq.Header.Set(key, value)
	}

	// Set remote address for logging
	if sourceIP := request.RequestContext.HTTP.SourceIP; sourceIP != "" {
		httpReq.RemoteAddr = sourceIP
	}

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Call the text extraction handler
	handlers.TextractHandler(recorder, httpReq)

	// Convert HTTP response to Lambda response
	resp := recorder.Result()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Failed to close response body", "error", err)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read response body", "error", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"Internal server error"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	// Convert headers
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
		Headers:    headers,
	}, nil
}

func main() {
	// Initialize structured logging for Lambda
	logging.InitLogger()

	slog.Info("Lambda function starting")
	lambda.Start(handleRequest)
}
