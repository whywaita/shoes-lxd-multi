package scheduler

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

func httpErrorResponse(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp := ErrorResponse{Error: err.Error()}
	jb, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, `{"error": "parse error"}`, http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(jb); err != nil {
		http.Error(w, `{"error": "write error"}`, http.StatusInternalServerError)
		return
	}
}

func (s *Scheduler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/schedule":
		s.handlePostSchedule(w, r)
	default:
		httpErrorResponse(w, fmt.Errorf("not found"), http.StatusNotFound)
	}
}

type ScheduleRequest struct {
	CPU    int `json:"cpu"`
	Memory int `json:"memory"`
}

type ScheduleResponse struct {
	Host string `json:"host"`
}

func (s *Scheduler) handlePostSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpErrorResponse(w, http.ErrNotSupported, http.StatusMethodNotAllowed)
		return
	}

	var req ScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErrorResponse(w, err, http.StatusBadRequest)
		return
	}

	host, ok := s.Schedule(req)
	if !ok {
		httpErrorResponse(w, fmt.Errorf("no available host"), http.StatusNotFound)
		return
	}

	resp := ScheduleResponse{Host: host}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		httpErrorResponse(w, err, http.StatusInternalServerError)
		return
	}
}
