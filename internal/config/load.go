package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const maxConfigBytes = 1 << 20

func Load(filename string) (Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, fmt.Errorf("load Action config %q: %w", filename, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return Config{}, fmt.Errorf("load Action config %q: %w", filename, errors.Join(readErr, closeErr))
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("load Action config %q: file exceeds %d bytes", filename, maxConfigBytes)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var value Config
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("decode Action config %q: %w", filename, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple YAML documents are not supported")
		}
		return Config{}, fmt.Errorf("decode Action config %q: %w", filename, err)
	}

	applyDefaults(&value)
	if err := validate(value); err != nil {
		return Config{}, fmt.Errorf("validate Action config %q: %w", filename, err)
	}
	return value, nil
}

func applyDefaults(value *Config) {
	if strings.TrimSpace(value.Project.Root) == "" {
		value.Project.Root = "."
	}
	if strings.TrimSpace(value.Project.BuildConfig) == "" {
		value.Project.BuildConfig = "lzc-build.yml"
	}
	if strings.TrimSpace(value.Project.PackageFile) == "" {
		value.Project.PackageFile = "package.yml"
	}
	if strings.TrimSpace(value.Project.Output) == "" {
		value.Project.Output = "dist/application.lpk"
	}
	if value.Update.Strategy == "" {
		value.Update.Strategy = StrategyPull
	}
	if value.Build.RunBuildScript == nil {
		enabled := true
		value.Build.RunBuildScript = &enabled
	}

	value.Project.Root = filepath.Clean(strings.TrimSpace(value.Project.Root))
	value.Project.BuildConfig = filepath.Clean(strings.TrimSpace(value.Project.BuildConfig))
	value.Project.PackageFile = filepath.Clean(strings.TrimSpace(value.Project.PackageFile))
	value.Project.Output = filepath.Clean(strings.TrimSpace(value.Project.Output))
	value.Update.Strategy = Strategy(strings.ToLower(strings.TrimSpace(string(value.Update.Strategy))))
	value.Update.VersionSource.Type = VersionSourceType(strings.ToLower(strings.TrimSpace(string(value.Update.VersionSource.Type))))
	value.Update.VersionSource.Image = strings.TrimSpace(value.Update.VersionSource.Image)
	for index := range value.Build.Toolchains {
		value.Build.Toolchains[index].Kind = strings.ToLower(strings.TrimSpace(value.Build.Toolchains[index].Kind))
		value.Build.Toolchains[index].Version = strings.TrimSpace(value.Build.Toolchains[index].Version)
	}
	for index := range value.Images {
		image := &value.Images[index]
		image.ID = strings.TrimSpace(image.ID)
		image.Target = strings.ToLower(strings.TrimSpace(image.Target))
		image.Service = strings.TrimSpace(image.Service)
		image.Source = strings.TrimSpace(image.Source)
		image.Channel = strings.ToLower(strings.TrimSpace(image.Channel))
		image.Sort = strings.ToLower(strings.TrimSpace(image.Sort))
		image.Delivery.Mode = strings.ToLower(strings.TrimSpace(image.Delivery.Mode))
	}
}

func validate(value Config) error {
	if value.Version != 1 {
		return fmt.Errorf("unsupported configuration version %d: expected 1", value.Version)
	}
	if err := validateRoot(value.Project.Root); err != nil {
		return err
	}
	for label, path := range map[string]string{
		"build_config": value.Project.BuildConfig,
		"package_file": value.Project.PackageFile,
		"output":       value.Project.Output,
	} {
		if err := validateProjectPath(label, path); err != nil {
			return err
		}
	}
	if !strings.EqualFold(filepath.Ext(value.Project.Output), ".lpk") {
		return errors.New("output must use the .lpk extension")
	}
	switch value.Update.Strategy {
	case StrategyPull, StrategyPublish:
	default:
		return fmt.Errorf("unsupported update strategy %q", value.Update.Strategy)
	}
	switch value.Update.VersionSource.Type {
	case VersionSourceGit:
		if value.Update.VersionSource.Image != "" {
			return errors.New("version source image must be empty when type is git")
		}
	case VersionSourceImage:
		return errors.New("version source image automation is not available in milestone 1")
	default:
		return fmt.Errorf("unsupported version source type %q", value.Update.VersionSource.Type)
	}

	toolchains := make(map[string]struct{}, len(value.Build.Toolchains))
	for _, toolchain := range value.Build.Toolchains {
		if toolchain.Kind == "" {
			return errors.New("toolchain kind is required")
		}
		if _, exists := toolchains[toolchain.Kind]; exists {
			return fmt.Errorf("duplicate toolchain kind %q", toolchain.Kind)
		}
		toolchains[toolchain.Kind] = struct{}{}
	}
	images := make(map[string]struct{}, len(value.Images))
	for _, image := range value.Images {
		if image.ID == "" {
			return errors.New("image id is required")
		}
		if _, exists := images[image.ID]; exists {
			return fmt.Errorf("duplicate image id %q", image.ID)
		}
		images[image.ID] = struct{}{}
	}
	return nil
}

func validateRoot(root string) error {
	if root == "" || filepath.IsAbs(root) || root == ".." || strings.HasPrefix(root, ".."+string(filepath.Separator)) {
		return errors.New("project root must be a repository-relative directory")
	}
	return nil
}

func validateProjectPath(label, path string) error {
	if path == "" || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must remain beneath project root", label)
	}
	return nil
}
