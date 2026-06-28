package fly

import (
	"context"
	"fmt"
	"net/http"
)

// BuilderAPI is the subset of the Fly Apps REST API used to provision the Fly
// App shell and build workspace images. Defining it as an interface keeps the
// orchestration layer testable without real Fly infrastructure.
type BuilderAPI interface {
	// EnsureApp creates the Fly App if it does not already exist (idempotent).
	EnsureApp(ctx context.Context, app string) error
	// BuildImage builds the given Dockerfile and pushes it to the Fly.io
	// internal registry, returning the resulting image reference.
	BuildImage(ctx context.Context, app, dockerfileContents string) (string, error)
}

// EnsureApp creates a Fly App under the configured organization. A 409
// (already exists) is treated as success so this is safe to call repeatedly.
func (c *Client) EnsureApp(ctx context.Context, app string) error {
	url := fmt.Sprintf("%s/v1/apps", c.appsBase)
	body := map[string]any{
		"app_name":     app,
		"organization": c.org,
	}
	err := c.do(ctx, http.MethodPost, url, body, nil)
	if err == nil {
		return nil
	}
	if ae, ok := err.(*APIError); ok && ae.Status == http.StatusConflict {
		return nil
	}
	return err
}

// BuildImage submits the Dockerfile to the Fly Apps build API and returns the
// image reference in the Fly.io internal registry. The build is pushed to
// `registry.fly.io/<app>:latest`, the registry path Fly uses for org-scoped
// images, which is what workspace machines are configured to boot.
//
// Note: Fly's remote build API is invoked here over the Apps REST endpoint. The
// exact request shape has historically evolved; this client targets the current
// documented endpoint and returns a deterministic image ref on success so the
// orchestration layer remains stable.
func (c *Client) BuildImage(ctx context.Context, app, dockerfileContents string) (string, error) {
	if app == "" {
		return "", fmt.Errorf("app name required")
	}
	if dockerfileContents == "" {
		return "", fmt.Errorf("dockerfile contents required")
	}
	imageRef := fmt.Sprintf("registry.fly.io/%s:latest", app)
	url := fmt.Sprintf("%s/v1/apps/%s/builds", c.appsBase, app)
	body := map[string]any{
		"dockerfile": dockerfileContents,
		"image":      imageRef,
		"tags":       []string{imageRef},
	}
	if err := c.do(ctx, http.MethodPost, url, body, nil); err != nil {
		return "", err
	}
	return imageRef, nil
}
