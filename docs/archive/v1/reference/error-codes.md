# Error codes

Common error codes you might see.

## Auth errors

| Code | Meaning |
|---|---|
| 401 | Missing or invalid auth credentials |
| 403 | You don't have permission |
| 4011 | Service secret is missing |
| 4012 | Service secret is invalid |
| 4031 | Device not paired |
| 4032 | Approval denied |

## Config errors

| Code | Meaning |
|---|---|
| 1001 | Config file not found |
| 1002 | Invalid config format |
| 1003 | Missing required field |
| 1004 | Provider not configured |

## Provider errors

| Code | Meaning |
|---|---|
| 2001 | Provider rate limit hit |
| 2002 | Provider auth failure (bad API key) |
| 2003 | Provider returned an error |
| 2004 | Model not available |

## Tool execution errors

| Code | Meaning |
|---|---|
| 3001 | Tool not found |
| 3002 | Tool execution failed |
| 3003 | Tool blocked by safety settings |
| 3004 | Invalid tool arguments |

## General errors

| Code | Meaning |
|---|---|
| 5000 | Internal server error |
| 5001 | Database error |
| 5002 | Channel connection failed |

This list is not exhaustive. Check the logs for more details on any error.
