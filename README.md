# Go Textract API

A Golang HTTP API for extracting text from documents and images using AWS Textract, featuring API key authentication and designed for serverless deployment on AWS Lambda.

## Features

- ✅ Pure Go implementation using only `net/http` (no external frameworks)
- ✅ API key authentication via `X-API-Key` header
- ✅ Health check endpoint at `/health`
- ✅ Text extraction endpoint at `/extract` using AWS Textract
- ✅ Multiple file format support:
  - **PDFs**: Multi-page documents (up to 10 pages per file)
  - **Images**: PNG, JPEG/JPG, TIFF (single page)
- ✅ Multiple file processing in a single request
- ✅ MIME type detection for accurate file type identification
- ✅ Concurrent file and page processing with worker pool pattern
- ✅ Graceful shutdown support
- ✅ Request logging middleware
- ✅ Comprehensive unit and integration tests
- ✅ AWS Lambda deployment ready via CloudFormation
- ✅ Dual deployment: Run locally as HTTP server or deploy to AWS Lambda

## Project Structure

```
/
├── cmd/
│   ├── api/
│   │   └── main.go           # Local server entry point
│   └── lambda/
│       └── main.go           # AWS Lambda entry point
├── internal/
│   ├── handlers/
│   │   ├── health.go         # Health check handler
│   │   ├── textract.go       # Text extraction handler (PDFs & images)
│   │   ├── health_test.go    # Health handler tests
│   │   └── textract_test.go  # Text extraction tests
│   ├── middleware/
│   │   ├── auth.go           # Authentication middleware
│   │   └── auth_test.go      # Middleware tests
│   └── server/
│       ├── server.go         # Server setup and configuration
│       └── server_test.go    # Server tests
├── tests/
│   ├── e2e_test.go           # End-to-end integration tests
│   └── data/                 # Test data files (PDFs, images)
├── cloudformation/
│   └── template.yml          # CloudFormation deployment template
├── Taskfile.yml              # Task runner configuration
├── .air.toml                 # Hot reload configuration
├── Dockerfile                # Docker configuration
├── .gitignore                # Git ignore file
├── go.mod                    # Go module file
└── README.md                 # This file
```

## Local Development Setup

### Prerequisites

- Go 1.21 or higher
- Git
- [Task](https://taskfile.dev) - Task runner (recommended)
- AWS CLI v2 configured with credentials (required for deployment)
- (Optional) Docker for containerized deployment

### Installation

1. Clone the repository:
```bash
git clone https://github.com/nicobistolfi/go-textract-api.git
cd go-textract-api
```

2. Install dependencies:
```bash
go mod download
# or using Task
task mod
```

3. Create a `.env` file from the example:
```bash
cp .env.example .env
# Edit .env and set your API_KEY
```

4. Install Task runner (if not already installed):
```bash
# macOS
brew install go-task/tap/go-task

# Linux
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d

# Windows (using Scoop)
scoop install task
```

## Quick Start with Task

View all available tasks:
```bash
task --list
# or simply
task
```

Common operations:
```bash
# Run the server locally
task run

# Run tests
task test

# Run tests with coverage
task test-coverage

# Build the binary
task build

# Format code
task fmt

# Start development server with hot reload
task dev
```

## Environment Variables Configuration

The application uses the following environment variables:

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `API_KEY` | API key for authentication | - | Yes |
| `PORT` | Port to run the server on | `8080` | No |
| `AWS_REGION` | AWS region for Textract service | - | Yes (for text extraction) |
| `AWS_ACCESS_KEY_ID` | AWS access key ID | - | Yes (unless using IAM roles) |
| `AWS_SECRET_ACCESS_KEY` | AWS secret access key | - | Yes (unless using IAM roles) |
| `ENV` | Environment (development/dev for debug logging) | `production` | No |
| `DEBUG` | Enable debug logging (set to "true") | `false` | No |

## Running the Server Locally

### Using Task (Recommended)

```bash
# Run with default dev API key
task run

# Run with custom API key
API_KEY="your-secret-api-key" task run

# Run on custom port
PORT=3000 task run
```

### Using Go directly

1. Set the required environment variables:
```bash
export API_KEY="your-secret-api-key"
export PORT="8080"  # Optional, defaults to 8080
```

2. Run the server:
```bash
go run cmd/api/main.go
```

### Using Docker

```bash
# Build and run in Docker
API_KEY="your-secret-api-key" task docker
```

The server will start on `http://localhost:8080` (or the port specified).

### Testing the API

Health check (no authentication required):
```bash
curl http://localhost:8080/health
# or using Task
curl http://localhost:8080/health | jq .
```

Expected response:
```json
{"status":"ok"}
```

With authentication (for future authenticated endpoints):
```bash
curl -H "X-API-Key: your-secret-api-key" http://localhost:8080/some-endpoint
# or using Task with custom API key
API_KEY="your-secret-api-key" task run
```

## Running Tests

### Using Task (Recommended)

```bash
# Run all tests
task test

# Run tests with coverage
task test-coverage

# Run end-to-end tests (requires AWS credentials)
task test:e2e

# Run linter
task lint

# Clean build artifacts
task clean
```

### Using Go directly

Run all tests with coverage:
```bash
go test -v -cover ./...
```

Run tests for a specific package:
```bash
go test -v ./internal/handlers
go test -v ./internal/middleware
go test -v ./internal/server
```

Run end-to-end tests (requires AWS credentials):
```bash
# Set up AWS credentials first
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
export AWS_REGION="us-west-1"

# Run e2e tests
go test -v ./tests
```

Generate coverage report:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## CloudFormation Deployment

Deployment is handled entirely by AWS CloudFormation — the stack definition lives in `cloudformation/template.yml` and creates:

- A Lambda function (`provided.al2` runtime, x86_64, 512 MB, 15s timeout)
- An IAM role granting CloudWatch Logs and `textract:DetectDocumentText`
- A CloudWatch log group with 14-day retention
- An API Gateway v2 HTTP API with a `$default` route integrated with the Lambda

### Prerequisites

1. AWS CLI v2 installed and configured:
```bash
aws configure
```

2. A `.env` file (or exported env vars) with at least `API_KEY` set. Optional: `ENV`, `DEBUG`, `STAGE`, `REGION`, `ARTIFACT_BUCKET`.

### Deploy

```bash
# Deploy using values from .env (defaults: STAGE=dev, REGION=us-west-1)
task deploy

# Override stage / region / artifact bucket
STAGE=production REGION=us-east-1 task deploy
ARTIFACT_BUCKET=my-existing-bucket task deploy

# One-shot with explicit values
API_KEY="your-api-key" ENV=development DEBUG=true task deploy
```

`task deploy` runs `cf:package` then `cf:deploy`:

- `cf:package` builds the Lambda binary (`task build:lambda`), zips it into `bootstrap.zip`, ensures an artifact S3 bucket exists (default name: `go-textract-api-artifacts-<account-id>-<region>`), then uses `aws cloudformation package` to upload the zip and rewrite `Code` references.
- `cf:deploy` runs `aws cloudformation deploy` against stack `go-textract-api-<stage>` with `CAPABILITY_NAMED_IAM`, then prints the stack outputs (including the HTTP API URL).

### Direct CLI (without Task)

```bash
# 1. Build + zip
GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o bootstrap ./cmd/lambda
zip -j bootstrap.zip bootstrap

# 2. Package (uploads the zip to S3)
aws cloudformation package \
  --template-file cloudformation/template.yml \
  --s3-bucket <your-artifact-bucket> \
  --output-template-file cloudformation/template.packaged.yml \
  --region us-west-1

# 3. Deploy
aws cloudformation deploy \
  --template-file cloudformation/template.packaged.yml \
  --stack-name go-textract-api-dev \
  --parameter-overrides Stage=dev ApiKey="$API_KEY" Env=production Debug=false \
  --capabilities CAPABILITY_NAMED_IAM \
  --region us-west-1
```

### Viewing Logs

```bash
task logs                  # tails /aws/lambda/go-textract-api-dev
STAGE=production task logs
```

Or directly:

```bash
aws logs tail /aws/lambda/go-textract-api-dev --follow --region us-west-1
```

### Removing the Deployment

```bash
task cf:remove
STAGE=production task cf:remove
```

Or directly:

```bash
aws cloudformation delete-stack --stack-name go-textract-api-dev --region us-west-1
```

Note: the artifact S3 bucket is not deleted automatically — remove it manually if you no longer need it.

## API Documentation

### Endpoints

#### `GET /health`
Health check endpoint that returns the service status.

**Authentication**: Not required

**Response:**
- Status: `200 OK`
- Body: `{"status": "ok"}`

#### `POST /extract`
Extract text from documents and images using AWS Textract.

**Authentication**: Required (X-API-Key header)

**Supported File Formats:**
- **PDFs**: Multi-page documents (max 10 pages per file)
- **Images**: PNG, JPEG/JPG, TIFF (single page)

**Request:**
- Method: `POST`
- Content-Type: `multipart/form-data`
- Form field: `files` - One or more files (max 10MB per file)
- File detection: Uses MIME type detection (not file extension)

**Response:**
- Status: `200 OK`
- Body:
```json
{
  "files": [
    {
      "filename": "document.pdf",
      "pages": [
        {
          "page_number": 1,
          "text": "extracted text from page 1"
        },
        {
          "page_number": 2,
          "text": "extracted text from page 2"
        }
      ],
      "total_pages": 2,
      "success": true
    },
    {
      "filename": "image.png",
      "pages": [
        {
          "page_number": 1,
          "text": "extracted text from image"
        }
      ],
      "total_pages": 1,
      "success": true
    }
  ],
  "success": true
}
```

**Error Responses:**
- `400 Bad Request` - No files provided or invalid request format
- `401 Unauthorized` - Missing or invalid API key
- Individual file errors are included in the response with `success: false` for that file

**Examples:**

Single file:
```bash
curl -X POST \
  -H "X-API-Key: your-api-key" \
  -F "files=@document.pdf" \
  http://localhost:8080/extract
```

Multiple files:
```bash
curl -X POST \
  -H "X-API-Key: your-api-key" \
  -F "files=@document.pdf" \
  -F "files=@image.png" \
  -F "files=@scan.jpg" \
  http://localhost:8080/extract
```

### Authentication

All endpoints (except `/health`) require API key authentication via the `X-API-Key` header.

**Example:**
```bash
curl -H "X-API-Key: your-api-key" https://your-api-url.com/endpoint
```

**Error Responses:**
- `401 Unauthorized` - Missing or invalid API key
  - `{"error": "Missing API key"}`
  - `{"error": "Invalid API key"}`
  - `{"error": "API key not configured"}`

## Development Guidelines

### Debug Logging

To enable detailed response logging in development:

```bash
# Using environment variable
ENV=development task run

# Or using DEBUG flag
DEBUG=true task run

# Or set both for maximum verbosity
ENV=development DEBUG=true task run
```

When debug logging is enabled, the API will:
- Log the full JSON response from the `/extract` endpoint
- Show preview of extracted text (first 500 characters per page)
- Include additional debug information for troubleshooting

**Note**: Debug logging should only be used in development as it may expose sensitive extracted text in logs.

### Development Tools

Install development dependencies:
```bash
go install github.com/cosmtrek/air@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

This installs:
- `air` - Hot reload for development
- `golangci-lint` - Linting tool

Start development server with hot reload:
```bash
task dev
```

Run code checks:
```bash
# Format code
task fmt

# Run linter (if installed)
task lint

# Run default task (format, test, build)
task default
```

Clean build artifacts:
```bash
task clean
```

## Text Extraction Capabilities

### Supported File Types

The API automatically detects file types using MIME type detection (not file extensions):

- **PDF Documents**: 
  - Multi-page support (up to 10 pages per file)
  - Automatic page splitting and concurrent processing
  - Full text extraction from complex layouts

- **Image Files**:
  - PNG (Portable Network Graphics)
  - JPEG/JPG (Joint Photographic Experts Group)
  - TIFF (Tagged Image File Format)
  - Single page processing per image
  - Optical Character Recognition (OCR)

### Processing Features

- **Concurrent Processing**: Multiple files and PDF pages are processed simultaneously using goroutines
- **Worker Pool Pattern**: Configurable worker pool limits to manage resource usage
- **MIME Type Detection**: Accurate file type identification based on content, not file extension
- **Error Handling**: Individual file processing errors don't affect other files in the same request
- **Scalable Architecture**: Designed to handle multiple files efficiently

### Adding New Endpoints

1. Create a new handler in `internal/handlers/`
2. Add authentication by wrapping with `middleware.AuthMiddleware()`
3. Register the route in `internal/server/server.go`
4. Write comprehensive tests

Example:
```go
// In internal/server/server.go
mux.HandleFunc("/api/users", middleware.AuthMiddleware(handlers.UsersHandler))
```

### Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Keep functions small and focused
- Write tests for all new functionality
- Use meaningful variable and function names

## Troubleshooting

### Common Issues

1. **Server fails to start**
   - Check if the port is already in use
   - Ensure all environment variables are set correctly

2. **Authentication failures**
   - Verify the `API_KEY` environment variable is set
   - Check that the `X-API-Key` header matches exactly

3. **Deployment issues**
   - Ensure AWS credentials are configured (`aws sts get-caller-identity`)
   - Confirm the IAM principal can create CloudFormation stacks, Lambda, IAM roles, API Gateway v2, and S3 buckets
   - Verify the Lambda binary was built for `linux/amd64` (see `task build:lambda`)

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
