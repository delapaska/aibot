package httpapi

import (
	"net/http"
	"time"

	"github.com/delapaska/aibot/internal/domain"
)

// userDTO is the wire format of a family member. display_name carries
// the panel-ready label so the frontend never reimplements the fallback
// logic (first name → username → id).
type userDTO struct {
	ID          int64     `json:"id"`
	FirstName   string    `json:"first_name"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

func toUserDTO(u domain.User) userDTO {
	return userDTO{
		ID:          u.ID,
		FirstName:   u.FirstName,
		Username:    u.Username,
		DisplayName: u.DisplayName(),
		CreatedAt:   u.CreatedAt,
	}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.users.List(r.Context())
	if err != nil {
		s.failErr(w, err)
		return
	}
	dto := make([]userDTO, len(users))
	for i, u := range users {
		dto[i] = toUserDTO(u)
	}
	writeJSON(w, http.StatusOK, listUsersResponse{Items: dto})
}

type listUsersResponse struct {
	Items []userDTO `json:"items"`
}
