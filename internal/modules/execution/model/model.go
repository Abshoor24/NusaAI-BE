package model

import "time"

// ExecuteRequest is the client payload for POST /api/v1/execute.
type ExecuteRequest struct {
	Language string `json:"language"`
	Code     string `json:"code"`
	Stdin    string `json:"stdin,omitempty"`
}

// ExecuteResponse is the execution result returned to the client.
type ExecuteResponse struct {
	ID              string `json:"id"`
	Language        string `json:"language"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	ExecutionTimeMs int64  `json:"execution_time_ms"`
	MemoryKB        int64  `json:"memory_kb"`
	Status          string `json:"status"`
}

// CodeExecution represents the database record saved in table code_executions.
type CodeExecution struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Language        string    `json:"language"`
	Code            string    `json:"code"`
	Stdin           string    `json:"stdin,omitempty"`
	Stdout          string    `json:"stdout"`
	Stderr          string    `json:"stderr"`
	ExitCode        int       `json:"exit_code"`
	ExecutionTimeMs int64     `json:"execution_time_ms"`
	MemoryKB        int64     `json:"memory_kb"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}
