package fly

import (
	"context"
	"fmt"
	"net/http"
)

// MachinesAPI is the subset of the Fly Machines REST API the orchestrator
// depends on. Defining it as an interface keeps orchestration unit-testable
// with a mock instead of real Firecracker machines.
type MachinesAPI interface {
	CreateMachine(ctx context.Context, app string, req CreateMachineRequest) (*Machine, error)
	StartMachine(ctx context.Context, app, machineID string) error
	StopMachine(ctx context.Context, app, machineID string) error
	DestroyMachine(ctx context.Context, app, machineID string) error
	GetMachine(ctx context.Context, app, machineID string) (*Machine, error)
	CreateVolume(ctx context.Context, app string, req CreateVolumeRequest) (*Volume, error)
	DestroyVolume(ctx context.Context, app, volumeID string) error
}

// Machine represents a Fly Firecracker microVM.
type Machine struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Region string `json:"region"`
	Image  string `json:"image_ref,omitempty"`
	Config struct {
		Image    string `json:"image,omitempty"`
		Services []Service `json:"services,omitempty"`
	} `json:"config"`
}

// Service describes a port exposed through the Fly Proxy. The `autostop` and
// `autostart` fields enable scale-to-zero: the machine suspends when idle and
// is woken by the edge proxy on the next request to its URL.
type Service struct {
	// InternalPort is the port the container listens on (code-server: 8080).
	InternalPort int `json:"internal_port"`
	// Protocol is the L4 protocol (typically "tcp").
	Protocol string `json:"protocol"`
	// Autostop, when set to "suspend", stops the machine (preserving the
	// volume) when all connections drop. Note: Fly's field is intentionally
	// spelled "autostop".
	Autostop string `json:"autostop,omitempty"`
	// Autostart enables the edge proxy to wake a suspended machine on demand.
	Autostart bool `json:"autostart"`
}

// Mount attaches a Fly Volume to a path inside the machine.
type Mount struct {
	// Volume is the Fly Volume id.
	Volume string `json:"volume"`
	// Path is the in-container mount point.
	Path string `json:"path"`
}

// Guest describes machine sizing.
type Guest struct {
	CPUClass string `json:"cpu_class,omitempty"`
	CPUs     int    `json:"cpus"`
	MemoryMB int    `json:"memory_mb"`
}

// MachineConfig is the `config` object in a create-machine request.
type MachineConfig struct {
	Image    string            `json:"image"`
	Guest    Guest             `json:"guest,omitempty"`
	Mounts   []Mount           `json:"mounts,omitempty"`
	Services []Service         `json:"services,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	// AutoDestroy tears down the machine when it stops; we intentionally leave
	// this false so suspended machines survive scale-to-zero.
	AutoDestroy bool `json:"auto_destroy,omitempty"`
	Restart     *struct {
		Policy string `json:"policy,omitempty"`
	} `json:"restart,omitempty"`
}

// CreateMachineRequest is the body of POST /v1/apps/{app}/machines.
type CreateMachineRequest struct {
	Name   string        `json:"name,omitempty"`
	Region string        `json:"region,omitempty"`
	Config MachineConfig `json:"config"`
}

// CreateMachine provisions a new Firecracker machine under the given Fly app.
func (c *Client) CreateMachine(ctx context.Context, app string, req CreateMachineRequest) (*Machine, error) {
	if app == "" {
		return nil, fmt.Errorf("app name required")
	}
	if req.Config.Image == "" {
		return nil, fmt.Errorf("machine image required")
	}
	url := fmt.Sprintf("%s/v1/apps/%s/machines", c.machinesBase, app)
	var m Machine
	if err := c.do(ctx, http.MethodPost, url, req, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// StartMachine starts a stopped/suspended machine.
func (c *Client) StartMachine(ctx context.Context, app, machineID string) error {
	url := fmt.Sprintf("%s/v1/apps/%s/machines/%s/start", c.machinesBase, app, machineID)
	return c.do(ctx, http.MethodPost, url, nil, nil)
}

// StopMachine stops a running machine (without destroying it).
func (c *Client) StopMachine(ctx context.Context, app, machineID string) error {
	url := fmt.Sprintf("%s/v1/apps/%s/machines/%s/stop", c.machinesBase, app, machineID)
	return c.do(ctx, http.MethodPost, url, nil, nil)
}

// DestroyMachine permanently destroys a machine.
func (c *Client) DestroyMachine(ctx context.Context, app, machineID string) error {
	url := fmt.Sprintf("%s/v1/apps/%s/machines/%s", c.machinesBase, app, machineID)
	return c.do(ctx, http.MethodDelete, url, nil, nil)
}

// GetMachine inspects a machine, returning its current state.
func (c *Client) GetMachine(ctx context.Context, app, machineID string) (*Machine, error) {
	url := fmt.Sprintf("%s/v1/apps/%s/machines/%s", c.machinesBase, app, machineID)
	var m Machine
	if err := c.do(ctx, http.MethodGet, url, nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
