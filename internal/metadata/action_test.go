package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestActionAndReleaseMetadataAreValidYAML(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"action.yml", ".goreleaser.yml", ".github/workflows/ci.yml", ".github/workflows/lazycat.yml", ".github/workflows/release.yml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if len(document) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestReleaseWorkflowSkipsFloatingMajorTag(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "if: github.ref_name != 'v1'") {
		t.Fatal("release workflow must not publish a second release for the floating v1 tag")
	}
}

func TestReleaseWorkflowRejectsBootstrapVersionMismatch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{"Verify Action bootstrap version", "LAZYCAT_ACTION_VERSION", "github.ref_name", "action.yml"} {
		if !strings.Contains(text, required) {
			t.Fatalf("release workflow is missing bootstrap version gate %q", required)
		}
	}
}

func TestReusableWorkflowContractAndActionRefs(t *testing.T) {
	filename := filepath.Join("..", "..", ".github", "workflows", "lazycat.yml")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	on, ok := document["on"].(map[string]any)
	if !ok {
		t.Fatalf("workflow on=%#v", document["on"])
	}
	call, ok := on["workflow_call"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_call=%#v", on["workflow_call"])
	}
	inputs, _ := call["inputs"].(map[string]any)
	for _, name := range []string{"config", "operation", "runner", "image-id", "dry-run", "changelog", "token-file", "toolchains", "go-version", "node-version", "rust-toolchain", "node-package-manager", "enable-qemu"} {
		if _, found := inputs[name]; !found {
			t.Fatalf("missing workflow input %q", name)
		}
	}
	secrets, _ := call["secrets"].(map[string]any)
	for _, name := range []string{"LAZYCAT_TOKEN", "LZC_CLI_TOKEN", "LAZYCAT_USERNAME", "LAZYCAT_PASSWORD", "APPSTORE_URL", "APPSTORE_TOKEN", "APP_ID", "PRIVATE_STORE_GROUP_CODES", "REGISTRY", "REGISTRY_USERNAME", "REGISTRY_PASSWORD"} {
		if _, found := secrets[name]; !found {
			t.Fatalf("missing workflow secret %q", name)
		}
	}
	outputs, _ := call["outputs"].(map[string]any)
	for _, name := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "store-results", "official-store-enabled", "private-store-enabled", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if _, found := outputs[name]; !found {
			t.Fatalf("missing workflow output %q", name)
		}
	}
	permissions, _ := document["permissions"].(map[string]any)
	for _, name := range []string{"contents", "pull-requests"} {
		if got := permissions[name]; got != "write" {
			t.Fatalf("workflow permission %q=%#v, want write", name, got)
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "uses: ") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "uses: "))
		value = strings.Fields(value)[0]
		if value == "ca-x/lazycat-github-action@v1" {
			continue
		}
		parts := strings.Split(value, "@")
		if len(parts) != 2 || !isMajorTag(parts[1]) {
			t.Fatalf("third-party Action must use a major vN tag: %s", value)
		}
	}
	workflow := string(data)
	for _, condition := range []string{
		"steps.lazycat.outputs.operation == 'check' && steps.lazycat.outputs.changed == 'true' && steps.lazycat.outputs.update-strategy == 'pull'",
		"steps.lazycat.outputs.operation == 'check' && steps.lazycat.outputs.changed == 'true' && steps.lazycat.outputs.update-strategy == 'publish'",
	} {
		if !strings.Contains(workflow, condition) {
			t.Fatalf("workflow is missing operation-based update condition %q", condition)
		}
	}
	if strings.Contains(workflow, "outputs.channel != ''") {
		t.Fatal("workflow must not use channel presence to classify check operations")
	}
	privateIndex := strings.Index(workflow, "- name: Publish to MiaoMiao private store")
	officialIndex := strings.Index(workflow, "- name: Publish to LazyCat official platform")
	if privateIndex < 0 || officialIndex < 0 || privateIndex > officialIndex {
		t.Fatal("idempotent private-store publishing must run before official publishing")
	}
	mergeIndex := strings.Index(workflow, "- name: Merge store publishing results")
	if mergeIndex < officialIndex {
		t.Fatal("store result merge must run after official publishing")
	}
	if !strings.Contains(workflow[privateIndex:officialIndex], "PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}") {
		t.Fatal("private publish step does not receive PRIVATE_STORE_GROUP_CODES from a reusable-workflow secret")
	}
	officialStep := workflow[officialIndex:mergeIndex]
	for _, contract := range []string{
		"if: ${{ !cancelled() && steps.lazycat.outputs.update-strategy == 'publish' && steps.lazycat.outputs.official-store-enabled == 'true'",
		"continue-on-error: ${{ steps.lazycat.outputs.private-store-enabled == 'true' }}",
	} {
		if !strings.Contains(officialStep, contract) {
			t.Fatalf("official publish step is missing isolation contract %q", contract)
		}
	}
	mergeRest := workflow[mergeIndex:]
	mergeEnd := strings.Index(mergeRest, "\n      - name: ")
	if mergeEnd < 0 {
		mergeEnd = len(mergeRest)
	}
	mergeStep := mergeRest[:mergeEnd]
	for _, contract := range []string{
		"if: ${{ always() && !cancelled() }}",
		"OFFICIAL_OUTCOME: ${{ steps.publish-official.outcome }}",
		`failureReason: 'official-publish-failed'`,
		"core.warning('Official store publication failed; other configured store results are preserved.')",
		"core.summary",
	} {
		if !strings.Contains(mergeStep, contract) {
			t.Fatalf("store result merge is missing partial-failure contract %q", contract)
		}
	}
	mergeRawIndex := strings.Index(mergeStep, "Object.assign(result, parsed);")
	mergeFailureIndex := strings.Index(mergeStep, "if (process.env.OFFICIAL_OUTCOME === 'failure')")
	if mergeRawIndex < 0 || mergeFailureIndex < mergeRawIndex {
		t.Fatal("official failure marker must override any partial Action output")
	}
	for _, condition := range []string{
		"steps.lazycat.outputs.update-strategy == 'publish' && steps.lazycat.outputs.official-store-enabled == 'true'",
		"steps.lazycat.outputs.update-strategy == 'publish' && steps.lazycat.outputs.private-store-enabled == 'true' && steps.store-artifact.outputs.download-url != ''",
	} {
		if !strings.Contains(workflow, condition) {
			t.Fatalf("workflow is missing store condition %q", condition)
		}
	}
	managedPaths := "add-paths: |\n            ${{ steps.lazycat.outputs.package-file }}\n            ${{ steps.lazycat.outputs.manifest-file }}"
	if !strings.Contains(workflow, managedPaths) {
		t.Fatal("workflow PR does not restrict changes to the managed package and Manifest paths")
	}
}

func isMajorTag(value string) bool {
	if len(value) < 2 || value[0] != 'v' {
		return false
	}
	for _, character := range value[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func TestReusableWorkflowPreparesVersionedReleaseAssets(t *testing.T) {
	filename := filepath.Join("..", "..", ".github", "workflows", "lazycat.yml")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	on, _ := document["on"].(map[string]any)
	call, _ := on["workflow_call"].(map[string]any)
	inputs, _ := call["inputs"].(map[string]any)
	input, ok := inputs["versioned-release-asset"].(map[string]any)
	if !ok {
		t.Fatal("workflow input versioned-release-asset is missing")
	}
	if got := input["type"]; got != "boolean" {
		t.Fatalf("versioned-release-asset type=%#v, want boolean", got)
	}
	if got := input["required"]; got != false {
		t.Fatalf("versioned-release-asset required=%#v, want false", got)
	}
	if got := input["default"]; got != false {
		t.Fatalf("versioned-release-asset default=%#v, want false", got)
	}

	workflow := string(data)
	preparedPath := "${{ steps.release-asset.outputs.lpk-path }}"
	prepareIndex := strings.Index(workflow, "- name: Prepare Release asset")
	classifyIndex := strings.Index(workflow, "- name: Classify Release work")
	inspectIndex := strings.Index(workflow, "- name: Inspect existing Release Asset")
	if prepareIndex < 0 || classifyIndex < 0 || inspectIndex < 0 || classifyIndex > prepareIndex || prepareIndex > inspectIndex {
		t.Fatal("Release asset preparation must run after classification and before inspection")
	}
	prepareRest := workflow[prepareIndex:]
	prepareEnd := strings.Index(prepareRest, "\n      - name: ")
	if prepareEnd < 0 {
		prepareEnd = len(prepareRest)
	}
	prepareStep := prepareRest[:prepareEnd]
	for _, contract := range []string{
		"if: steps.release-state.outputs.should-release == 'true'",
		"LPK_PATH: ${{ steps.lazycat.outputs.lpk-path }}",
		"PACKAGE_ID: ${{ steps.lazycat.outputs.package-id }}",
		"VERSION: ${{ steps.lazycat.outputs.version }}",
		"VERSIONED_RELEASE_ASSET: ${{ inputs.versioned-release-asset }}",
		`if [[ -z "${LPK_PATH}" || -z "${PACKAGE_ID}" || -z "${VERSION}" ]]`,
		`if [[ ! "${PACKAGE_ID}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ || ! "${VERSION}" =~ ^[0-9][0-9A-Za-z.+-]*$ ]]`,
		`asset_path="${LPK_PATH}"`,
		`if [[ "${VERSIONED_RELEASE_ASSET}" == "true" ]]`,
		`asset_dir="$(dirname -- "${LPK_PATH}")"`,
		`asset_path="${asset_dir}/${PACKAGE_ID}-v${VERSION}.lpk"`,
		`cp -- "${LPK_PATH}" "${asset_path}"`,
		`delimiter="lazycat_release_asset"`,
		`while grep -Fxq "${delimiter}" <<<"${asset_path}"; do`,
		`echo "lpk-path<<${delimiter}"`,
		`printf '%s\n' "${asset_path}"`,
		`echo "${delimiter}"`,
		`} >>"${GITHUB_OUTPUT}"`,
	} {
		if !strings.Contains(prepareStep, contract) {
			t.Fatalf("Release asset preparation is missing contract %q", contract)
		}
	}
	if strings.Contains(prepareStep, "GITHUB_WORKSPACE") || strings.Contains(prepareStep, "RUNNER_TEMP") || strings.Contains(prepareStep, `echo "lpk-path=${asset_path}"`) {
		t.Fatal("Release asset preparation must stay beside the verified LPK and use multiline outputs")
	}
	for _, name := range []string{
		"- name: Inspect existing Release Asset",
		"- name: Upload GitHub Release Asset",
		"- name: Resolve Release Asset URL",
	} {
		start := strings.Index(workflow, name)
		if start < 0 {
			t.Fatalf("workflow step %q is missing", name)
		}
		rest := workflow[start+len(name):]
		end := strings.Index(rest, "\n      - name: ")
		if end < 0 {
			end = len(rest)
		}
		if !strings.Contains(rest[:end], preparedPath) {
			t.Fatalf("workflow step %q does not use the prepared Release asset", name)
		}
	}
	for _, contract := range []string{
		"- name: Locate existing Release Asset for store reconciliation",
		"const assetName = `${packageId}-v${version}.lpk`;",
		"/^sha256:[0-9a-f]{64}$/",
		"- name: Download existing Release Asset for store reconciliation",
		`gh release download "${RELEASE_TAG}" --pattern "${ASSET_NAME}"`,
		`if [[ "${actual_sha256}" != "${EXPECTED_SHA256}" ]]`,
		"- name: Select verified store artifact",
		"lpk-path: ${{ steps.store-artifact.outputs.lpk-path }}",
		"download-url: ${{ steps.store-artifact.outputs.download-url }}",
		"sha256: ${{ steps.store-artifact.outputs.sha256 }}",
	} {
		if !strings.Contains(workflow, contract) {
			t.Fatalf("Release/store reconciliation is missing contract %q", contract)
		}
	}
	artifactIndex := strings.Index(workflow, "- name: Upload validation Artifact")
	if artifactIndex < 0 {
		t.Fatal("validation Artifact upload is missing")
	}
	artifactRest := workflow[artifactIndex:]
	artifactEnd := strings.Index(artifactRest, "\n      - name: ")
	if artifactEnd < 0 {
		artifactEnd = len(artifactRest)
	}
	artifactStep := artifactRest[:artifactEnd]
	if !strings.Contains(artifactStep, "path: ${{ steps.lazycat.outputs.lpk-path }}") || strings.Contains(artifactStep, preparedPath) {
		t.Fatal("validation Artifact upload must keep the Action's original LPK path")
	}
}

func TestActionMetadataExposesStableContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Inputs  map[string]any `yaml:"inputs"`
		Outputs map[string]any `yaml:"outputs"`
		Runs    struct {
			Using string `yaml:"using"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"operation", "config", "image-id", "version", "changelog", "lpk-path", "download-url", "sha256", "token-file", "dry-run"} {
		if _, exists := document.Inputs[input]; !exists {
			t.Fatalf("missing input %q", input)
		}
	}
	if _, exists := document.Inputs["private-group-codes"]; exists {
		t.Fatal("private group codes must be a secret/environment variable, not an Action input")
	}
	for _, output := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "store-results", "official-store-enabled", "private-store-enabled", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if _, exists := document.Outputs[output]; !exists {
			t.Fatalf("missing output %q", output)
		}
	}
	if document.Runs.Using != "composite" {
		t.Fatalf("runs.using=%q", document.Runs.Using)
	}
	if !strings.Contains(string(data), "LAZYCAT_ACTION_VERSION: v1.1.14") {
		t.Fatal("action.yml must bootstrap release v1.1.14")
	}
}

func TestActionMetadataUsesBracketSyntaxForHyphenatedNames(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expression := range []string{
		"steps.run.outputs['package-id']",
		"steps.run.outputs['package-file']",
		"steps.run.outputs['manifest-file']",
		"steps.run.outputs['target-platform']",
		"steps.run.outputs['update-strategy']",
		"inputs['image-id']",
		"inputs['download-url']",
		"inputs['token-file']",
	} {
		if !strings.Contains(text, expression) {
			t.Fatalf("action.yml is missing safe expression %q", expression)
		}
	}
}
