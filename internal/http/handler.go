package http

import (
	"encoding/json"
	nethttp "net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/event-fanout-service/event-fanout/internal/models"
	"github.com/event-fanout-service/event-fanout/internal/service"
)

// Handler wraps HTTP handlers with required services
type Handler struct {
	eventService        *service.EventService
	subscriptionService *service.SubscriptionService
	logger              *zap.Logger
}

// NewHandler creates a new HTTP handler
func NewHandler(
	eventService *service.EventService,
	subscriptionService *service.SubscriptionService,
	logger *zap.Logger,
) *Handler {
	return &Handler{
		eventService:        eventService,
		subscriptionService: subscriptionService,
		logger:              logger,
	}
}

// RegisterRoutes registers all HTTP routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/health", h.HealthCheck).Methods("GET")
	router.HandleFunc("/api/v1/events", h.IngestEvent).Methods("POST")
	router.HandleFunc("/api/v1/subscriptions", h.CreateSubscription).Methods("POST")
	router.HandleFunc("/api/v1/subscriptions", h.ListSubscriptions).Methods("GET")
	router.HandleFunc("/api/v1/subscriptions/{subId}", h.GetSubscription).Methods("GET")
	router.HandleFunc("/api/v1/subscriptions/{subId}", h.UpdateSubscription).Methods("PUT")
	router.HandleFunc("/api/v1/subscriptions/{subId}", h.DeleteSubscription).Methods("DELETE")
}

// HealthCheck handles health check
func (h *Handler) HealthCheck(w nethttp.ResponseWriter, r *nethttp.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.HealthCheckResponse{
		Status:   "healthy",
		Database: true,
		Redis:    true,
		Message:  "Event fanout service is running",
	})
}

// IngestEvent handles POST /api/v1/events
func (h *Handler) IngestEvent(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req models.CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, nethttp.StatusBadRequest, "invalid request body")
		return
	}

	if req.Type == "" || req.Source == "" {
		h.errorResponse(w, nethttp.StatusBadRequest, "type and source are required")
		return
	}

	if req.Payload == nil {
		req.Payload = json.RawMessage(`{}`)
	}

	event, err := h.eventService.IngestEvent(r.Context(), &req)
	if err != nil {
		h.logger.Error("failed to ingest event", zap.Error(err))
		h.errorResponse(w, nethttp.StatusInternalServerError, "failed to ingest event")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusCreated)
	json.NewEncoder(w).Encode(event)
}

// CreateSubscription handles POST /api/v1/subscriptions
func (h *Handler) CreateSubscription(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req models.CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, nethttp.StatusBadRequest, "invalid request body")
		return
	}

	sub, err := h.subscriptionService.CreateSubscription(r.Context(), &req)
	if err != nil {
		h.logger.Error("failed to create subscription", zap.Error(err))
		h.errorResponse(w, nethttp.StatusInternalServerError, "failed to create subscription")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusCreated)
	json.NewEncoder(w).Encode(sub)
}

// ListSubscriptions handles GET /api/v1/subscriptions
func (h *Handler) ListSubscriptions(w nethttp.ResponseWriter, r *nethttp.Request) {
	subs, err := h.subscriptionService.ListSubscriptions(r.Context())
	if err != nil {
		h.logger.Error("failed to list subscriptions", zap.Error(err))
		h.errorResponse(w, nethttp.StatusInternalServerError, "failed to list subscriptions")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if subs == nil {
		subs = []models.Subscription{}
	}
	json.NewEncoder(w).Encode(subs)
}

// GetSubscription handles GET /api/v1/subscriptions/{subId}
func (h *Handler) GetSubscription(w nethttp.ResponseWriter, r *nethttp.Request) {
	vars := mux.Vars(r)
	subID, err := uuid.Parse(vars["subId"])
	if err != nil {
		h.errorResponse(w, nethttp.StatusBadRequest, "invalid subscription ID")
		return
	}

	sub, err := h.subscriptionService.GetSubscription(r.Context(), subID)
	if err != nil {
		h.logger.Error("failed to get subscription", zap.Error(err))
		h.errorResponse(w, nethttp.StatusNotFound, "subscription not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// UpdateSubscription handles PUT /api/v1/subscriptions/{subId}
func (h *Handler) UpdateSubscription(w nethttp.ResponseWriter, r *nethttp.Request) {
	vars := mux.Vars(r)
	subID, err := uuid.Parse(vars["subId"])
	if err != nil {
		h.errorResponse(w, nethttp.StatusBadRequest, "invalid subscription ID")
		return
	}

	var req models.CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, nethttp.StatusBadRequest, "invalid request body")
		return
	}

	sub, err := h.subscriptionService.UpdateSubscription(r.Context(), subID, &req)
	if err != nil {
		h.logger.Error("failed to update subscription", zap.Error(err))
		h.errorResponse(w, nethttp.StatusInternalServerError, "failed to update subscription")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// DeleteSubscription handles DELETE /api/v1/subscriptions/{subId}
func (h *Handler) DeleteSubscription(w nethttp.ResponseWriter, r *nethttp.Request) {
	vars := mux.Vars(r)
	subID, err := uuid.Parse(vars["subId"])
	if err != nil {
		h.errorResponse(w, nethttp.StatusBadRequest, "invalid subscription ID")
		return
	}

	if err := h.subscriptionService.DeleteSubscription(r.Context(), subID); err != nil {
		h.logger.Error("failed to delete subscription", zap.Error(err))
		h.errorResponse(w, nethttp.StatusInternalServerError, "failed to delete subscription")
		return
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

// errorResponse writes an error response
func (h *Handler) errorResponse(w nethttp.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
