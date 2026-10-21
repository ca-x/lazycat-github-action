package deployparams_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/deployparams"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestValidateDeployParams(t *testing.T) {
	const valid = "params:\n  - id: auth\n    type: bool\n    name: Authentication\n    description: Require login\n    default_value: 'false'\n    optional: true\n"
	tests := []struct {
		name string
		data string
		want string
	}{
		{"valid bool", valid, ""},
		{"YAML bool default", strings.Replace(valid, "'false'", "false", 1), ""},
		{"YAML capitalized bool", strings.Replace(valid, "'false'", "False", 1), ""},
		{"empty list", "params: []\n", ""},
		{"empty document", "", "valid YAML"},
		{"list root", "[]", "must be a mapping"},
		{"mapping params", "params: {}", "params"},
		{"missing params", "locales: {}", "params"},
		{"invalid type", strings.Replace(valid, "type: bool", "type: enum", 1), "params[0].type"},
		{"unsupported options", valid + "    options: []\n", "unsupported field"},
		{"unsupported root field", valid + "unexpected: value\n", "unsupported field"},
		{"missing id", strings.Replace(valid, "id: auth", "id: ''", 1), "params[0].id"},
		{"missing type", strings.Replace(valid, "    type: bool\n", "", 1), "params[0].type"},
		{"duplicate id", valid + "  - id: auth\n    type: string\n", "params[1].id"},
		{"duplicate field", valid + "    type: string\n", "duplicate mapping key"},
		{"wrong optional type", strings.Replace(valid, "optional: true", "optional: 'true'", 1), "params[0].optional"},
		{"invalid explicit bool tag", strings.Replace(valid, "optional: true", "optional: !!bool invalid", 1), "params[0].optional"},
		{"wrong hidden type", valid + "    hidden: 1\n", "params[0].hidden"},
		{"wrong name type", strings.Replace(valid, "name: Authentication", "name: []", 1), "params[0].name"},
		{"invalid bool default", strings.Replace(valid, "'false'", "'off'", 1), "params[0].default_value"},
		{"invalid explicitly typed default", strings.Replace(valid, "'false'", "!!bool invalid", 1), "params[0].default_value"},
		{"wrong string default type", strings.Replace(strings.Replace(valid, "type: bool", "type: string", 1), "'false'", "[]", 1), "params[0].default_value"},
		{"locale unknown id", valid + "locales:\n  zh:\n    missing:\n      name: 认证\n", "existing parameter id"},
		{"locale unsupported options", valid + "locales:\n  zh:\n    auth:\n      options: []\n", "unsupported field"},
		{"locale wrong description type", valid + "locales:\n  zh:\n    auth:\n      description: false\n", "description"},
		{"locale duplicate id", valid + "locales:\n  zh:\n    auth: {}\n    auth: {}\n", "duplicate mapping key"},
		{"locale wrong shape", valid + "locales: []\n", "must be a mapping"},
		{"multiple documents", valid + "---\nparams: []\n", "exactly one YAML document"},
		{"syntax error", "params: [\n", "valid YAML"},
		{"alias", "params: &params\n  - id: auth\n    type: bool\n  - *params\n", "must be a mapping"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := deployparams.Validate([]byte(test.data), "lzc-deploy-params.yml")
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) || !errors.Is(err, lpkgo.ErrInvalidManifest) {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
		})
	}
}

func TestValidateSupportedTypesAndOpaqueDefaults(t *testing.T) {
	data := `params:
  - id: listen.port
    type: string
    default_value: "8080"
  - id: owner
    type: lzc_uid
  - id: password
    type: secret
    default_value: $random(len=24)
    hidden: true
locales:
  zh:
    password:
      name: 密码
      description: 自动生成密码
`
	if err := deployparams.Validate([]byte(data), "custom/params.yml"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDoesNotExposeValues(t *testing.T) {
	const secret = "private-value-must-not-appear"
	for _, data := range []string{
		"params:\n  - id: password\n    type: " + secret + "\n",
		"params:\n  - id: password\n    type: bool\n    default_value: " + secret + "\n",
		"params: []\n" + secret + ": value\n",
		"params: []\nlocales:\n  zh:\n    " + secret + ": {}\n",
		"params: ['" + secret + "]\n",
	} {
		err := deployparams.Validate([]byte(data), "lzc-deploy-params.yml")
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("unsafe or missing error: %v", err)
		}
	}
}
