# Jobs Endpoints

Endpoints for background job management. Jobs run independently of the request that created them.

## Create Job

`POST /api/v1/jobs`

```json
{
  "type": "research",
  "input": {
    "query": "latest developments in AI",
    "depth": "thorough"
  }
}
```

Returns the job ID and initial status.

## Get Job Status

`GET /api/v1/jobs/:id`

Returns the job's current status, any output so far, and error details if failed.

## List Jobs

`GET /api/v1/jobs`

Lists recent jobs. Supports filtering by status and type. Uses cursor-based pagination.

## Cancel Job

`DELETE /api/v1/jobs/:id`

Cancels a running job. Jobs that are pending or running can be cancelled. Completed or failed jobs return an error.

## Job Statuses

- pending — waiting to start
- running — being processed
- completed — finished successfully
- failed — stopped with an error
- cancelled — aborted by user
