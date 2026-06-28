package fly

import (
	"context"
	"fmt"
	"net/http"
)

// Volume represents an NVMe Fly Volume used for persistent /workspace storage.
type Volume struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SizeGB   int    `json:"size_gb"`
	Region   string `json:"region"`
	Encrypted bool  `json:"encrypted"`
}

// CreateVolumeRequest is the body of POST /v1/apps/{app}/volumes.
type CreateVolumeRequest struct {
	Name      string `json:"name"`
	Region    string `json:"region"`
	SizeGB    int    `json:"size_gb"`
	Encrypted bool   `json:"encrypted"`
}

// CreateVolume provisions a new NVMe Fly Volume under the given app. The
// returned Volume.ID is later referenced by a machine mount to attach the
// volume at /workspace.
func (c *Client) CreateVolume(ctx context.Context, app string, req CreateVolumeRequest) (*Volume, error) {
	if app == "" {
		return nil, fmt.Errorf("app name required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("volume name required")
	}
	url := fmt.Sprintf("%s/v1/apps/%s/volumes", c.machinesBase, app)
	var v Volume
	if err := c.do(ctx, http.MethodPost, url, req, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DestroyVolume deletes a Fly Volume. Volumes should be detached (machine
// destroyed) first.
func (c *Client) DestroyVolume(ctx context.Context, app, volumeID string) error {
	url := fmt.Sprintf("%s/v1/apps/%s/volumes/%s", c.machinesBase, app, volumeID)
	return c.do(ctx, http.MethodDelete, url, nil, nil)
}
