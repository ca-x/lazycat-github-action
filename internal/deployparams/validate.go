// Package deployparams validates the installation parameters described by
// https://developer.lazycat.cloud/spec/deploy-params.html.
package deployparams

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ca-x/lazycat-github-action/internal/diagnostic"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"go.yaml.in/yaml/v3"
)

// MaxDocumentBytes bounds deployment metadata reads in source files and LPKs.
const MaxDocumentBytes = 8 << 20

type validationError struct {
	source string
	detail string
}

func (err *validationError) Error() string             { return err.source + ": " + err.detail }
func (err *validationError) Unwrap() error             { return lpkgo.ErrInvalidManifest }
func (err *validationError) PublicErrorPath() string   { return err.source }
func (err *validationError) PublicErrorDetail() string { return err.detail }

type validator struct{ source string }

func (v validator) fail(node *yaml.Node, field, problem string) error {
	line := ""
	if node != nil {
		line = fmt.Sprintf(" (line %d)", node.Line)
	}
	return &validationError{source: v.source, detail: field + line + ": " + problem}
}

// Validate checks a single YAML document without evaluating defaults or templates.
// Diagnostics identify fields and lines, but never include parameter values.
func Validate(data []byte, source string) error {
	v := validator{source: diagnostic.SafePath(source)}
	if len(data) > MaxDocumentBytes {
		return v.fail(nil, "$", "deployment parameter document exceeds 8 MiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		problem := "deployment parameters must be valid YAML"
		if detail := diagnostic.SafeYAMLSyntaxDetail(err); detail != "" {
			problem += ": " + detail
		}
		return v.fail(nil, "$", problem)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return v.fail(nil, "$", "deployment parameters must contain exactly one YAML document")
	}
	if len(document.Content) != 1 {
		return v.fail(nil, "$", "deployment parameters must be a mapping")
	}
	fields, err := v.mapping(document.Content[0], "$", []string{"params", "locales"})
	if err != nil {
		return err
	}
	params := fields["params"]
	if params == nil || params.Kind != yaml.SequenceNode || params.Tag != "!!seq" {
		return v.fail(params, "params", "must be a sequence")
	}
	ids := make(map[string]bool, len(params.Content))
	for index, param := range params.Content {
		field := fmt.Sprintf("params[%d]", index)
		values, err := v.mapping(param, field, []string{"id", "type", "name", "description", "optional", "default_value", "hidden"})
		if err != nil {
			return err
		}
		for _, name := range []string{"id", "type"} {
			if node := values[name]; !isString(node) || strings.TrimSpace(node.Value) == "" {
				return v.fail(param, field+"."+name, "must be a non-empty string")
			}
		}
		id := values["id"].Value
		if ids[id] {
			return v.fail(values["id"], field+".id", "must be unique within params")
		}
		ids[id] = true
		kind := values["type"].Value
		if !slices.Contains([]string{"bool", "lzc_uid", "string", "secret"}, kind) {
			return v.fail(values["type"], field+".type", "unsupported parameter type; see https://developer.lazycat.cloud/spec/deploy-params.html")
		}
		for _, name := range []string{"name", "description"} {
			if node := values[name]; node != nil && !isString(node) {
				return v.fail(node, field+"."+name, "must be a string")
			}
		}
		for _, name := range []string{"optional", "hidden"} {
			if node := values[name]; node != nil && !isBoolean(node) {
				return v.fail(node, field+"."+name, "must be a YAML boolean")
			}
		}
		if node := values["default_value"]; node != nil {
			// Existing LazyCat projects use both quoted and YAML boolean defaults.
			if kind == "bool" {
				if !isBoolean(node) && (!isString(node) || (node.Value != "true" && node.Value != "false")) {
					return v.fail(node, field+".default_value", "bool defaults must be true or false, optionally quoted")
				}
			} else if !isString(node) {
				return v.fail(node, field+".default_value", "must be a string")
			}
		}
	}
	if locales := fields["locales"]; locales != nil {
		return v.locales(locales, ids)
	}
	return nil
}

func (v validator) mapping(node *yaml.Node, field string, allowed []string) (map[string]*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode || node.Tag != "!!map" {
		return nil, v.fail(node, field, "must be a mapping")
	}
	fields := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		if !isString(key) || strings.TrimSpace(key.Value) == "" {
			return nil, v.fail(key, field, "mapping keys must be non-empty strings")
		}
		if _, duplicate := fields[key.Value]; duplicate {
			return nil, v.fail(key, field, "duplicate mapping key")
		}
		if allowed != nil && !slices.Contains(allowed, key.Value) {
			return nil, v.fail(key, field, "unsupported field; allowed fields: "+strings.Join(allowed, ", "))
		}
		fields[key.Value] = value
	}
	return fields, nil
}

func (v validator) locales(node *yaml.Node, ids map[string]bool) error {
	if _, err := v.mapping(node, "locales", nil); err != nil {
		return err
	}
	// Use positions for dynamic locale/parameter keys so diagnostics cannot
	// expose arbitrary repository strings through the Action's public errors.
	for i := 0; i < len(node.Content); i += 2 {
		locale := node.Content[i+1]
		field := fmt.Sprintf("locales[%d]", i/2)
		if _, err := v.mapping(locale, field, nil); err != nil {
			return err
		}
		for j := 0; j < len(locale.Content); j += 2 {
			key, value := locale.Content[j], locale.Content[j+1]
			translationField := fmt.Sprintf("%s[%d]", field, j/2)
			if !ids[key.Value] {
				return v.fail(key, translationField, "translation must reference an existing parameter id")
			}
			fields, err := v.mapping(value, translationField, []string{"name", "description"})
			if err != nil {
				return err
			}
			for _, name := range []string{"name", "description"} {
				if fieldNode := fields[name]; fieldNode != nil && !isString(fieldNode) {
					return v.fail(fieldNode, translationField+"."+name, "must be a string")
				}
			}
		}
	}
	return nil
}

func isString(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!str"
}

func isBoolean(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!bool" &&
		(strings.EqualFold(node.Value, "true") || strings.EqualFold(node.Value, "false"))
}
