package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/delapaska/aibot/internal/domain"
)

// writeJSON sends v as a JSON body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the uniform error envelope of the API:
//
//	{"error": {"code": "not_found", "message": "..."}}
//
// Machine-readable codes let the panel show precise messages without
// parsing human text.
type errorBody struct {
	Error errInfo `json:"error"`
}

type errInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: errInfo{Code: code, Message: msg}})
}

// failErr maps err to an HTTP status and writes the envelope. Internal
// errors are logged in full but reported to the client without details,
// so internals never leak through the API.
func (s *Server) failErr(w http.ResponseWriter, err error) {
	status, code := mapError(err)
	if status >= http.StatusInternalServerError {
		s.logger.Error("request failed", "error", err)
		fail(w, status, code, "internal error")
		return
	}
	fail(w, status, code, err.Error())
}

// mapError converts domain errors to status/code pairs. Unknown errors
// are treated as internal: the client gets a generic 500, the log gets
// the details.
func mapError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, domain.ErrInvalidUserID):
		return http.StatusBadRequest, "invalid_user_id"
	case errors.Is(err, domain.ErrInvalidAmount):
		return http.StatusBadRequest, "invalid_amount"
	case errors.Is(err, domain.ErrInvalidCategory):
		return http.StatusBadRequest, "invalid_category"
	case errors.Is(err, domain.ErrInvalidDate):
		return http.StatusBadRequest, "invalid_date"
	case errors.Is(err, domain.ErrInvalidComment):
		return http.StatusBadRequest, "invalid_comment"
	case errors.Is(err, domain.ErrInvalidName):
		return http.StatusBadRequest, "invalid_name"
	case errors.Is(err, domain.ErrInvalidDebtor):
		return http.StatusBadRequest, "invalid_debtor"
	case errors.Is(err, domain.ErrInvalidDirection):
		return http.StatusBadRequest, "invalid_direction"
	case errors.Is(err, domain.ErrAlreadySettled):
		return http.StatusConflict, "already_settled"
	default:
		return http.StatusInternalServerError, "internal"
	}
}
