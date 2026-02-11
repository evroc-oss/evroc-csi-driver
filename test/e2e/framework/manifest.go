package framework

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// LoadManifest loads a YAML manifest from a file with variable substitution
func (f *Framework) LoadManifest(path string, vars map[string]string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", wrapError(fmt.Sprintf("read manifest %s", path), err)
	}

	manifest := string(data)

	// Replace variables in the format ${VAR_NAME}
	for key, value := range vars {
		placeholder := fmt.Sprintf("${%s}", key)
		manifest = strings.ReplaceAll(manifest, placeholder, value)
	}

	return manifest, nil
}

// LoadAndApplyManifest loads and applies a manifest file
func (f *Framework) LoadAndApplyManifest(ctx context.Context, path string, vars map[string]string) error {
	manifest, err := f.LoadManifest(path, vars)
	if err != nil {
		return err
	}

	return f.Apply(ctx, manifest)
}
