package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultMaxBodyBytes int64 = 1 << 20

type Problem struct {
	Type          string            `json:"type"`
	Title         string            `json:"title"`
	Status        int               `json:"status"`
	Detail        string            `json:"detail,omitempty"`
	Instance      string            `json:"instance,omitempty"`
	CorrelationID string            `json:"correlationId,omitempty"`
	Errors        map[string]string `json:"errors,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteProblem(w http.ResponseWriter, problem Problem) {
	if problem.Type == "" {
		problem.Type = "about:blank"
	}
	if problem.Status == 0 {
		problem.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(problem)
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		return fmt.Errorf("content-type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}

func Method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			WriteProblem(w, Problem{Title: "Method not allowed", Status: http.StatusMethodNotAllowed})
			return
		}
		next(w, r)
	}
}
