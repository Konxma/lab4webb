package repository

import (
	"errors"
	"sort"
	"sync"

	"device-registry/internal/domain"
)

var ErrDuplicate = errors.New("device ID already exists")

type DeviceRepository interface {
	GetAll() []domain.Device
	Find(filter domain.DeviceFilter) []domain.Device
	GetByID(id string) (domain.Device, bool)
	Create(device domain.Device) error
	Update(id string, device domain.Device) bool
	Delete(id string) bool
}

type MemoryDeviceRepository struct {
	mu      sync.RWMutex
	devices map[string]domain.Device
}

func NewMemoryDeviceRepository() *MemoryDeviceRepository {
	return &MemoryDeviceRepository{devices: make(map[string]domain.Device)}
}

func (r *MemoryDeviceRepository) GetAll() []domain.Device {
	return r.Find(domain.DeviceFilter{})
}

func (r *MemoryDeviceRepository) Find(filter domain.DeviceFilter) []domain.Device {
	r.mu.RLock()
	devices := make([]domain.Device, 0)
	for _, device := range r.devices {
		if (filter.Status == "" || filter.Status == device.Status) &&
			(filter.Type == "" || filter.Type == device.Type) {
			devices = append(devices, device)
		}
	}
	r.mu.RUnlock()
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	return devices
}

func (r *MemoryDeviceRepository) GetByID(id string) (domain.Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	device, exists := r.devices[id]
	return device, exists
}

func (r *MemoryDeviceRepository) Create(device domain.Device) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.devices[device.ID]; exists {
		return ErrDuplicate
	}
	r.devices[device.ID] = device
	return nil
}

func (r *MemoryDeviceRepository) Update(id string, device domain.Device) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.devices[id]; !exists {
		return false
	}
	device.ID = id
	r.devices[id] = device
	return true
}

func (r *MemoryDeviceRepository) Delete(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.devices[id]; !exists {
		return false
	}
	delete(r.devices, id)
	return true
}
