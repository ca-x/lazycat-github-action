// Package releasenotes resolves application update notes from templates and exact GitHub releases.
package releasenotes

import (
	"errors"
	"reflect"
	"strings"
	"text/template"
	"text/template/parse"
)

// MaxChangelogBytes keeps resolved notes safe to forward through Action environment inputs.
const MaxChangelogBytes = 64 << 10

const DefaultTemplate = "{{if .ReleaseNotes}}{{.ReleaseNotes}}{{else}}Release {{.Version}}{{end}}"

type Data struct {
	Version      string
	Tag          string
	PackageID    string
	Name         string
	Homepage     string
	Repository   string
	ReleaseNotes string
	ReleaseURL   string
}

func ValidateTemplate(source string) error {
	if len(source) > MaxChangelogBytes {
		return errors.New("changelog template exceeds 64 KiB")
	}
	if strings.TrimSpace(source) == "" {
		source = DefaultTemplate
	}
	tmpl, err := template.New("changelog").Option("missingkey=error").Parse(source)
	if err != nil {
		return errors.New("invalid changelog template syntax")
	}
	for _, defined := range tmpl.Templates() {
		if err := validateNodes(defined.Tree.Root); err != nil {
			return err
		}
	}
	return nil
}

// Inspect every branch without evaluating functions against invented package data.
func validateNodes(nodes ...parse.Node) error {
	for _, node := range nodes {
		var children []parse.Node
		switch node := node.(type) {
		case *parse.ListNode:
			if node != nil {
				children = node.Nodes
			}
		case *parse.ActionNode:
			children = []parse.Node{node.Pipe}
		case *parse.PipeNode:
			for _, command := range node.Cmds {
				children = append(children, command)
			}
		case *parse.CommandNode:
			children = node.Args
		case *parse.IfNode:
			children = []parse.Node{node.Pipe, node.List, node.ElseList}
		case *parse.WithNode:
			children = []parse.Node{node.Pipe, node.List, node.ElseList}
		case *parse.RangeNode:
			children = []parse.Node{node.Pipe, node.List, node.ElseList}
		case *parse.TemplateNode:
			if node.Pipe != nil {
				children = []parse.Node{node.Pipe}
			}
		case *parse.FieldNode:
			if !validField(node.Ident) {
				return errors.New("unknown changelog template field")
			}
		case *parse.VariableNode:
			if len(node.Ident) > 1 && !validField(node.Ident[1:]) {
				return errors.New("unknown changelog template field")
			}
		case *parse.ChainNode:
			if !validField(node.Field) {
				return errors.New("unknown changelog template field")
			}
			children = []parse.Node{node.Node}
		}
		if err := validateNodes(children...); err != nil {
			return err
		}
	}
	return nil
}

func validField(path []string) bool {
	if len(path) != 1 {
		return false
	}
	_, ok := reflect.TypeFor[Data]().FieldByName(path[0])
	return ok
}

func Render(source string, data Data) (string, error) {
	if strings.TrimSpace(source) == "" {
		source = DefaultTemplate
	}
	tmpl, err := template.New("changelog").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", errors.New("invalid changelog template syntax")
	}
	var output boundedOutput
	if err := tmpl.Execute(&output, data); err != nil {
		return "", errors.New("unable to render changelog template; check fields and output size")
	}
	value := output.builder.String()
	if strings.TrimSpace(value) == "" {
		value = "Release " + data.Version
	}
	return value, nil
}

type boundedOutput struct{ builder strings.Builder }

func (output *boundedOutput) Write(data []byte) (int, error) {
	if len(data) > MaxChangelogBytes-output.builder.Len() {
		return 0, errors.New("changelog exceeds 64 KiB")
	}
	return output.builder.Write(data)
}
