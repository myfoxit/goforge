package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// APIError is the uniform JSON error shape of every GoForge endpoint.
type APIError struct {
	Status  int            `json:"status"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func (e *APIError) Error() string { return fmt.Sprintf("%d: %s", e.Status, e.Message) }

func NewAPIError(status int, msg string, data map[string]any) *APIError {
	return &APIError{Status: status, Message: msg, Data: data}
}

func BadRequest(msg string) *APIError {
	if msg == "" {
		msg = "Invalid request."
	}
	return NewAPIError(http.StatusBadRequest, msg, nil)
}

func ValidationError(field, msg string) *APIError {
	return NewAPIError(http.StatusBadRequest, "Validation failed.", map[string]any{field: msg})
}

func Unauthorized(msg string) *APIError {
	if msg == "" {
		msg = "Missing or invalid authentication."
	}
	return NewAPIError(http.StatusUnauthorized, msg, nil)
}

func Forbidden(msg string) *APIError {
	if msg == "" {
		msg = "You are not allowed to perform this action."
	}
	return NewAPIError(http.StatusForbidden, msg, nil)
}

func NotFound(msg string) *APIError {
	if msg == "" {
		msg = "The requested resource was not found."
	}
	return NewAPIError(http.StatusNotFound, msg, nil)
}

func TooManyRequests() *APIError {
	return NewAPIError(http.StatusTooManyRequests, "Too many requests, slow down.", nil)
}

// WriteJSON writes v as a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

// WriteError maps an error to the API error shape. Non-APIError values are
// hidden behind a generic 500 (details go to the log, not the client).
func WriteError(w http.ResponseWriter, log *slog.Logger, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		WriteJSON(w, apiErr.Status, apiErr)
		return
	}
	if log != nil {
		log.Error("internal error", "err", err)
	}
	WriteJSON(w, http.StatusInternalServerError, &APIError{
		Status:  http.StatusInternalServerError,
		Message: "Something went wrong while processing your request.",
	})
}

const maxBodySize = 32 << 20 // 32 MiB (multipart uploads are checked separately)

// ReadJSON decodes a JSON request body with a size cap.
func ReadJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		return BadRequest("Unable to read request body.")
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return BadRequest("Invalid JSON body: " + err.Error())
	}
	return nil
}
