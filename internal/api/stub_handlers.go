package api

import (
	"encoding/json"
	"net/http"
)

// StubHandler provides a stub implementation for unimplemented features
// returning HTTP 501 Not Implemented with a descriptive message
type StubHandler struct {
	feature string
}

// NewStubHandler creates a new stub handler for a feature
func NewStubHandler(feature string) *StubHandler {
	return &StubHandler{feature: feature}
}

// ServeHTTP returns 501 Not Implemented
func (h *StubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":       "Not Implemented",
		"message":     h.feature + " feature is not yet implemented",
		"status_code": http.StatusNotImplemented,
		"documentation": "See https://github.com/docker-secret-operator/dso/blob/main/ROADMAP.md for feature status",
	})
}

// StubRecommendationHandler provides stub responses for recommendation endpoints
type StubRecommendationHandler struct{}

// NewStubRecommendationHandler creates a new stub recommendation handler
func NewStubRecommendationHandler() *StubRecommendationHandler {
	return &StubRecommendationHandler{}
}

// ServeHTTP routes stub recommendation API requests
func (h *StubRecommendationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":       "Not Implemented",
		"message":     "Recommendation engine is not yet implemented in this version",
		"status_code": http.StatusNotImplemented,
		"documentation": "See https://github.com/docker-secret-operator/dso/blob/main/ROADMAP.md for feature status",
	})
}

// StubDriftHandler provides stub responses for drift detection endpoints
type StubDriftHandler struct{}

// NewStubDriftHandler creates a new stub drift handler
func NewStubDriftHandler() *StubDriftHandler {
	return &StubDriftHandler{}
}

// ServeHTTP routes stub drift API requests
func (h *StubDriftHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":       "Not Implemented",
		"message":     "Drift detection engine is not yet implemented in this version",
		"status_code": http.StatusNotImplemented,
		"documentation": "See https://github.com/docker-secret-operator/dso/blob/main/ROADMAP.md for feature status",
	})
}

// StubForecastHandler provides stub responses for forecasting endpoints
type StubForecastHandler struct{}

// NewStubForecastHandler creates a new stub forecast handler
func NewStubForecastHandler() *StubForecastHandler {
	return &StubForecastHandler{}
}

// ServeHTTP routes stub forecast API requests
func (h *StubForecastHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":       "Not Implemented",
		"message":     "Forecasting engine is not yet implemented in this version",
		"status_code": http.StatusNotImplemented,
		"documentation": "See https://github.com/docker-secret-operator/dso/blob/main/ROADMAP.md for feature status",
	})
}
