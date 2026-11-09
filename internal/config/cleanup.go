package config

import (
	"errors"

	"go.yaml.in/yaml/v3"
)

type ReleaseCleanup struct {
	Enabled    *bool           `yaml:"enabled"`
	KeepLatest *RetentionCount `yaml:"keep_latest"`
}

type RetentionCount int

func (count *RetentionCount) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag != "!!int" {
		return errors.New("release.cleanup.keep_latest must be a positive integer")
	}
	var value int
	if err := node.Decode(&value); err != nil {
		return err
	}
	*count = RetentionCount(value)
	return nil
}

func (cleanup ReleaseCleanup) ShouldCleanup() bool {
	return cleanup.Enabled == nil || *cleanup.Enabled
}

func (cleanup ReleaseCleanup) KeepLatestCount() int {
	if cleanup.KeepLatest == nil {
		return 1000
	}
	return int(*cleanup.KeepLatest)
}
