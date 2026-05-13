# Files Endpoints

Endpoints for file operations. The agent can upload, download, and manage files.

## Upload File

`POST /api/v1/files`

Multipart form upload. Returns file metadata including a file ID.

```json
{
  "file_id": "file_abc123",
  "name": "report.pdf",
  "size": 1024000,
  "content_type": "application/pdf"
}
```

## Download File

`GET /api/v1/files/:file_id`

Returns the file content with the correct content type. Supports range requests for partial downloads.

## List Files

`GET /api/v1/files`

Lists files in the artifacts directory. Supports filtering by type and date. Cursor-based pagination.

## Delete File

`DELETE /api/v1/files/:file_id`

Removes a file from storage. This is permanent.

## Storage

Files are stored in the artifacts directory. The directory is inside the storage path (`~/.or3-intern/artifacts/`). File metadata is in SQLite.

## File Size Limits

Maximum file size is configurable. Default is 100MB per file.
