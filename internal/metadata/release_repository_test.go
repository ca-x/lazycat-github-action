package metadata

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type releaseScriptResult struct {
	Outputs  map[string]string `json:"outputs"`
	Failures []string          `json:"failures"`
	Calls    []struct {
		Method string         `json:"method"`
		Args   map[string]any `json:"args"`
	} `json:"calls"`
}

// Execute the real workflow JavaScript against a destination-only API fixture.
// Any accidental request to the private source repository fails the test.
func runReleaseScript(t *testing.T, step string, environment map[string]string, state map[string]any) releaseScriptResult {
	t.Helper()
	with := licenseWorkflowStep(t, step)["with"].(map[string]any)
	fixture, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := `const state=JSON.parse(process.env.RELEASE_TEST_STATE);
 const outputs={}, failures=[], calls=[];
 const context={repo:{owner:'private',repo:'source'},eventName:state.event||'push',ref:state.ref||'refs/heads/source-main',payload:{repository:{default_branch:'source-main'}}};
 const core={setOutput:(k,v)=>outputs[k]=String(v),setFailed:msg=>failures.push(msg)};
 const call=(method,args,data,status)=>{
   calls.push({method,args});
   const expected=state.destination||'distribution/app-releases';
   if (args.owner+'/'+args.repo!==expected) throw new Error('unexpected source API request');
   if(status) throw Object.assign(new Error('private response must not escape'),{status});
   return {data};
 };
 const github={rest:{repos:{
   get:async args=>call('get',args,{default_branch:'release-main'},state.status),
   getCommit:async args=>call('getCommit',args,{sha:'destination-commit'},state.commitStatus||state.status),
   getReleaseByTag:async args=>call('getReleaseByTag',args,{assets:state.assets||[]},state.status)
 },git:{getRef:async args=>call('getRef',args,{object:{sha:'older-tag-commit'}},state.status)}}};
 const execute=async()=>{` + with["script"].(string) + `};
 (async()=>{await execute(); console.log(JSON.stringify({outputs,failures,calls}));})().catch(err=>{console.error(err);process.exit(1)});`
	command := exec.CommandContext(t.Context(), "node", "-e", wrapper)
	command.Env = append(os.Environ(), "RELEASE_TEST_STATE="+string(fixture))
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", step, err, output)
	}
	var result releaseScriptResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode %s: %v\n%s", step, err, output)
	}
	return result
}

func TestReleaseDestinationPreflight(t *testing.T) {
	for _, test := range []struct {
		name, repository, target, event, dryRun      string
		status, commitStatus                         int
		token                                        bool
		wantRepository, wantTarget, wantRef, failure string
		wantCalls                                    int
	}{
		{name: "same repository default", wantRepository: "private/source"},
		{name: "same repository ignores case", repository: "Private/Source", wantRepository: "Private/Source"},
		{name: "cross organization default branch", repository: "distribution/app-releases", token: true, wantRepository: "distribution/app-releases", wantTarget: "destination-commit", wantRef: "release-main", wantCalls: 2},
		{name: "cross organization explicit branch", repository: "distribution/app-releases", target: "artifacts", token: true, wantRepository: "distribution/app-releases", wantTarget: "destination-commit", wantRef: "artifacts", wantCalls: 1},
		{name: "same repository explicit ref", repository: "private/source", target: "release-branch", wantRepository: "private/source", wantTarget: "destination-commit", wantRef: "release-branch", wantCalls: 1},
		{name: "missing cross repository token", repository: "distribution/app-releases", failure: "GH_RELEASE_TOKEN"},
		{name: "dry run needs no token", repository: "distribution/app-releases", dryRun: "true", wantRepository: "distribution/app-releases"},
		{name: "fork PR needs no token", repository: "distribution/app-releases", event: "pull_request", wantRepository: "distribution/app-releases"},
		{name: "invalid repository", repository: "https://github.com/distribution/app-releases", failure: "release-repository"},
		{name: "path traversal", repository: "distribution/..", failure: "release-repository"},
		{name: "invalid ref", repository: "distribution/app-releases", target: "main\ninjected", token: true, failure: "release-target-commitish"},
		{name: "destination unavailable", repository: "distribution/app-releases", token: true, status: 404, failure: "HTTP 404", wantCalls: 1},
		{name: "empty destination", repository: "distribution/app-releases", token: true, commitStatus: 409, failure: "HTTP 409", wantCalls: 2},
		{name: "missing target ref", repository: "distribution/app-releases", target: "missing", token: true, commitStatus: 404, failure: "HTTP 404", wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := "false"
			if test.token {
				token = "true"
			}
			state := map[string]any{"event": test.event, "destination": test.wantRepository, "status": test.status, "commitStatus": test.commitStatus}
			if test.failure != "" {
				state["destination"] = test.repository
			}
			result := runReleaseScript(t, "Resolve Release destination", map[string]string{
				"RELEASE_REPOSITORY": test.repository, "RELEASE_TARGET": test.target,
				"HAS_RELEASE_TOKEN": token, "DRY_RUN": test.dryRun,
			}, state)
			if test.failure != "" {
				if len(result.Failures) != 1 || !strings.Contains(result.Failures[0], test.failure) || strings.Contains(result.Failures[0], "private response") {
					t.Fatalf("failures=%v", result.Failures)
				}
			} else if len(result.Failures) != 0 || result.Outputs["repository"] != test.wantRepository || result.Outputs["target-commitish"] != test.wantTarget {
				t.Fatalf("result=%+v", result)
			}
			if len(result.Calls) != test.wantCalls {
				t.Fatalf("calls=%v", result.Calls)
			}
			if test.wantRef != "" && result.Calls[len(result.Calls)-1].Args["ref"] != test.wantRef {
				t.Fatalf("target ref=%v want %s", result.Calls, test.wantRef)
			}
		})
	}
}

func TestReleaseTagLookupUsesPublicationRepository(t *testing.T) {
	for _, repository := range []string{"private/source", "distribution/app-releases"} {
		for _, exists := range []bool{false, true} {
			t.Run(repository+"/exists="+map[bool]string{false: "false", true: "true"}[exists], func(t *testing.T) {
				state := map[string]any{"destination": repository}
				if !exists {
					state["status"] = 404
				}
				result := runReleaseScript(t, "Check remote Release tag", map[string]string{"RELEASE_REPOSITORY": repository, "RELEASE_TAG": "component-v1.2.3"}, state)
				want := map[bool]string{false: "false", true: "true"}[exists]
				if result.Outputs["exists"] != want || len(result.Calls) != 1 || result.Calls[0].Args["ref"] != "tags/component-v1.2.3" {
					t.Fatalf("result=%+v", result)
				}
			})
		}
	}
}

func TestReleaseClassificationAndRecoveryUseDestination(t *testing.T) {
	for _, test := range []struct {
		name, strategy, ref, want string
		missing, asset            bool
	}{
		{name: "new direct publication", strategy: "publish", missing: true, want: "true"},
		{name: "missing LPK on existing release", strategy: "publish", want: "true"},
		{name: "existing LPK is reused", strategy: "publish", asset: true, want: "false"},
		{name: "source default branch builds release", strategy: "pull", missing: true, want: "true"},
		{name: "destination default branch is not source branch", strategy: "pull", ref: "refs/heads/release-main", want: "false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := map[string]any{"ref": test.ref}
			if test.missing {
				state["status"] = 404
			}
			if test.asset {
				state["assets"] = []map[string]any{{"name": "cloud.example.app-v1.2.3.lpk"}}
			}
			result := runReleaseScript(t, "Classify Release work", map[string]string{
				"RELEASE_REPOSITORY": "distribution/app-releases", "RELEASE_TAG": "v1.2.3", "LPK_PATH": "/build/app.lpk",
				"PACKAGE_ID": "cloud.example.app", "VERSION": "1.2.3", "VERSIONED_RELEASE_ASSET": "true", "UPDATE_STRATEGY": test.strategy,
			}, state)
			if len(result.Failures) != 0 || result.Outputs["should-release"] != test.want {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestPublicationAssetIdentityAndDigest(t *testing.T) {
	const url = "https://github.com/distribution/app-releases/releases/download/v1.2.3/cloud.example.app-v1.2.3.lpk"
	for _, name := range []string{"Inspect existing Release Asset", "Resolve Release Asset URL", "Locate existing Release Asset for store reconciliation"} {
		for _, digest := range []string{"sha256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("b", 64), ""} {
			t.Run(name+"/"+digest, func(t *testing.T) {
				result := runReleaseScript(t, name, map[string]string{
					"RELEASE_REPOSITORY": "distribution/app-releases", "RELEASE_TAG": "v1.2.3", "LPK_PATH": "/build/cloud.example.app-v1.2.3.lpk",
					"LPK_SHA256": strings.Repeat("a", 64), "PACKAGE_ID": "cloud.example.app", "VERSION": "1.2.3",
				}, map[string]any{"assets": []map[string]any{{"id": 42, "name": "cloud.example.app-v1.2.3.lpk", "digest": digest, "browser_download_url": url}}})
				valid := digest == "sha256:"+strings.Repeat("a", 64)
				if name == "Locate existing Release Asset for store reconciliation" {
					valid = digest != ""
				}
				if valid {
					if len(result.Failures) != 0 || result.Outputs["download-url"] != url {
						t.Fatalf("result=%+v", result)
					}
				} else if len(result.Failures) != 1 || result.Outputs["download-url"] != "" {
					t.Fatalf("unsafe asset accepted: %+v", result)
				}
			})
		}
	}
}

func TestReleaseCredentialAndSourceIsolationWiring(t *testing.T) {
	const repository = "${{ steps.release-destination.outputs.repository }}"
	const token = "${{ secrets.GH_RELEASE_TOKEN || github.token }}"
	for _, name := range []string{"Resolve Release destination", "Classify Release work", "Check remote Release tag", "Inspect existing Release Asset", "Resolve Release Asset URL", "Locate existing Release Asset for store reconciliation"} {
		step := licenseWorkflowStep(t, name)
		with := step["with"].(map[string]any)
		if with["github-token"] != token || with["retries"] != 3 {
			t.Fatalf("%s credentials/retries=%v", name, with)
		}
		if name != "Resolve Release destination" && step["env"].(map[string]any)["RELEASE_REPOSITORY"] != repository {
			t.Fatalf("%s destination=%v", name, step["env"])
		}
	}
	upload := licenseWorkflowStep(t, "Upload GitHub Release Asset")["with"].(map[string]any)
	if upload["repository"] != repository || upload["token"] != token {
		t.Fatalf("upload destination=%v", upload)
	}
	wantTarget := "${{ steps.release-tag.outputs.exists != 'true' && (steps.release-destination.outputs.target-commitish || steps.direct-commit.outputs.commit || github.sha) || '' }}"
	if upload["target_commitish"] != wantTarget {
		t.Fatalf("unsafe target commit: %v", upload["target_commitish"])
	}
	for _, name := range []string{"Checkout application", "Create update pull request", "Commit direct-publish update", "Sync tag/release version to default branch"} {
		step := licenseWorkflowStep(t, name)
		data, err := json.Marshal(step)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "GH_RELEASE_TOKEN") || strings.Contains(string(data), "release-destination") {
			t.Fatalf("source operation %s received destination credentials/repository", name)
		}
	}
	action := licenseWorkflowStep(t, "Run LazyCat automation")
	if action["with"].(map[string]any)["release-repository"] != repository || action["env"].(map[string]any)["GH_RELEASE_TOKEN"] != "${{ secrets.GH_RELEASE_TOKEN }}" {
		t.Fatal("publication identity and credential are not passed into changelog resolution")
	}
}

func TestReconciliationDownloadsFromPublicationRepository(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// A recording gh stub verifies the real shell script's repository and token.
	stub := `#!/usr/bin/env bash
set -euo pipefail
[[ "${GH_TOKEN}" == "release-token" ]]
[[ "$*" == 'release download v1.2.3 --pattern cloud.example.app-v1.2.3.lpk --repo distribution/app-releases --dir '* ]]
while (( $# )); do
  if [[ "$1" == '--dir' ]]; then
    shift
    printf 'verified bytes' > "$1/cloud.example.app-v1.2.3.lpk"
    break
  fi
  shift
done
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	step := licenseWorkflowStep(t, "Download existing Release Asset for store reconciliation")
	if step["env"].(map[string]any)["GH_TOKEN"] != "${{ secrets.GH_RELEASE_TOKEN || github.token }}" {
		t.Fatal("download did not receive release credential")
	}
	for _, digest := range []string{"186287b2d987891f027b4bc8baaf621a3e5a4a73ec78e04b0f65dc309b1ccc03", strings.Repeat("a", 64)} {
		command := exec.CommandContext(t.Context(), "bash", "-c", step["run"].(string))
		outputFile := filepath.Join(t.TempDir(), "github-output")
		command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "GITHUB_WORKSPACE="+root, "GITHUB_OUTPUT="+outputFile,
			"GH_TOKEN=release-token", "GH_REPO=distribution/app-releases", "RELEASE_TAG=v1.2.3", "ASSET_NAME=cloud.example.app-v1.2.3.lpk", "EXPECTED_SHA256="+digest)
		output, err := command.CombinedOutput()
		if digest == strings.Repeat("a", 64) {
			if err == nil || !strings.Contains(string(output), "SHA256 does not match") {
				t.Fatalf("digest mismatch accepted: %v\n%s", err, output)
			}
		} else if err != nil {
			t.Fatalf("download: %v\n%s", err, output)
		}
	}
}
