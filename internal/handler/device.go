package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"device-registry/internal/domain"
	"device-registry/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const devicesPath = "/api/v1/devices"

type deviceHandler struct {
	devices repository.DeviceRepository
}

type deviceInput struct {
	Name      string  `json:"name"`
	IPAddress string  `json:"ip_address"`
	Type      string  `json:"type"`
	Status    *string `json:"status"`
}

type errorResponse struct {
	Error     string    `json:"error"`
	Message   string    `json:"message"`
	Status    int       `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func problem(w http.ResponseWriter, code int, message string) {
	respond(w, code, errorResponse{http.StatusText(code), message, code, time.Now().UTC()})
}

func missingDevice(w http.ResponseWriter, id string) {
	problem(w, http.StatusNotFound, fmt.Sprintf("Device with ID %q does not exist in registry", id))
}

func validStatus(status string) bool {
	return status == "online" || status == "offline"
}

func (h deviceHandler) list(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		problem(w, http.StatusBadRequest, "Invalid query parameters")
		return
	}
	filter := domain.DeviceFilter{
		Status: strings.ToLower(strings.TrimSpace(query.Get("status"))),
		Type:   strings.ToLower(strings.TrimSpace(query.Get("type"))),
	}
	if filter.Status != "" && !validStatus(filter.Status) {
		problem(w, http.StatusBadRequest, "status must be online or offline")
		return
	}
	if filter == (domain.DeviceFilter{}) {
		respond(w, http.StatusOK, h.devices.GetAll())
		return
	}
	respond(w, http.StatusOK, h.devices.Find(filter))
}

func (h deviceHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, exists := h.devices.GetByID(id)
	if !exists {
		missingDevice(w, id)
		return
	}
	respond(w, http.StatusOK, item)
}

func (h deviceHandler) create(w http.ResponseWriter, r *http.Request) {
	item, ok := readDevice(w, r)
	if !ok {
		return
	}
	id, err := uuid.NewRandom()
	if err != nil {
		problem(w, http.StatusInternalServerError, "Could not generate a device ID")
		return
	}
	item.ID = id.String()
	item.UpdatedAt = time.Now().UTC()
	if err := h.devices.Create(item); err != nil {
		problem(w, http.StatusInternalServerError, "Could not store the device")
		return
	}
	w.Header().Set("Location", devicesPath+"/"+item.ID)
	respond(w, http.StatusCreated, item)
}

func (h deviceHandler) update(w http.ResponseWriter, r *http.Request) {
	item, ok := readDevice(w, r)
	if !ok {
		return
	}
	item.ID = chi.URLParam(r, "id")
	item.UpdatedAt = time.Now().UTC()
	if !h.devices.Update(item.ID, item) {
		missingDevice(w, item.ID)
		return
	}
	respond(w, http.StatusOK, item)
}

func (h deviceHandler) remove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.devices.Delete(id) {
		missingDevice(w, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readDevice(w http.ResponseWriter, r *http.Request) (domain.Device, bool) {
	var input *deviceInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&input)
	if err == nil {
		err = decoder.Decode(new(json.RawMessage))
		if errors.Is(err, io.EOF) && input != nil {
			return validateDevice(w, *input)
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		problem(w, http.StatusRequestEntityTooLarge, "Request body exceeds 16 KiB")
	} else {
		problem(w, http.StatusBadRequest, "Body must be one JSON object with fields name, ip_address, type and optional status")
	}
	return domain.Device{}, false
}

func validateDevice(w http.ResponseWriter, input deviceInput) (domain.Device, bool) {
	name := strings.TrimSpace(input.Name)
	deviceType := strings.ToLower(strings.TrimSpace(input.Type))
	if name == "" || deviceType == "" || strings.TrimSpace(input.IPAddress) == "" {
		problem(w, http.StatusBadRequest, "name, ip_address and type are required")
		return domain.Device{}, false
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(input.IPAddress))
	if err != nil || ip.Zone() != "" {
		problem(w, http.StatusBadRequest, "ip_address must be an IPv4 or IPv6 address")
		return domain.Device{}, false
	}
	status := "offline"
	if input.Status != nil {
		status = strings.ToLower(strings.TrimSpace(*input.Status))
		if !validStatus(status) {
			problem(w, http.StatusBadRequest, "status must be online or offline")
			return domain.Device{}, false
		}
	}
	return domain.Device{Name: name, IPAddress: ip.String(), Type: deviceType, Status: status}, true
}
