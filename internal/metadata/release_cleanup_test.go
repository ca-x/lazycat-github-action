package metadata

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func cleanupRelease(identifier int, tag string) map[string]any {
	return map[string]any{
		"id": identifier, "tag_name": tag, "draft": false,
		"published_at": time.Unix(int64(identifier)*3600, 0).UTC().Format(time.RFC3339),
	}
}

func runCleanupScript(testContext *testing.T, environment map[string]string, state map[string]any) releaseScriptResult {
	testContext.Helper()
	step := licenseWorkflowStep(testContext, "Clean up old GitHub Releases")
	with := step["with"].(map[string]any)
	fixture, err := json.Marshal(state)
	if err != nil {
		testContext.Fatal(err)
	}
	wrapper := `const state=JSON.parse(process.env.CLEANUP_TEST_STATE);
const outputs={},failures=[],calls=[];
const context={repo:{owner:'private',repo:'source'},eventName:state.event||'workflow_dispatch'};
const core={setOutput:(key,value)=>outputs[key]=String(value),setFailed:message=>failures.push(message),info:()=>{},warning:()=>{}};
const record=(method,args)=>{
  calls.push({method,args});
  if(args.owner+'/'+args.repo!=='distribution/app-releases') throw new Error('wrong repository');
};
const fail=status=>Object.assign(new Error('secret response must not escape'),{status});
const github={rest:{repos:{
  listReleases:()=>{},
  getLatestRelease:async args=>{record('latest',args);if(state.latestStatus)throw fail(state.latestStatus);return {data:state.latest||{id:5}};},
  deleteRelease:async args=>{record('delete',args);if(state.deleteStatus)throw fail(state.deleteStatus);return {status:204};}
}},paginate:{iterator:async function*(_endpoint,args){
  for(let index=0;index<(state.pages||[]).length;index++){
    record('list',{...args,page:index+1});
    if(state.listStatus&&index===(state.failPage||0))throw fail(state.listStatus);
    yield {data:state.pages[index]};
  }
}}};
const execute=async()=>{` + with["script"].(string) + `};
(async()=>{await execute();console.log(JSON.stringify({outputs,failures,calls}));})().catch(error=>{console.error(error);process.exit(1)});`
	command := exec.CommandContext(testContext.Context(), "node", "-e", wrapper)
	command.Env = append(os.Environ(), "CLEANUP_TEST_STATE="+string(fixture))
	settings := map[string]string{
		"CLEANUP_ENABLED": "true", "KEEP_LATEST": "2", "DRY_RUN": "false",
		"RELEASE_REPOSITORY": "distribution/app-releases", "CURRENT_TAG": "v5",
	}
	for key, value := range environment {
		settings[key] = value
	}
	for key, value := range settings {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		testContext.Fatalf("cleanup script: %v\n%s", err, output)
	}
	var result releaseScriptResult
	if err := json.Unmarshal(output, &result); err != nil {
		testContext.Fatalf("cleanup result: %v\n%s", err, output)
	}
	return result
}

func cleanupDeletedIDs(result releaseScriptResult) []int {
	var identifiers []int
	for _, call := range result.Calls {
		if call.Method == "delete" {
			identifiers = append(identifiers, int(call.Args["release_id"].(float64)))
		}
	}
	return identifiers
}

func TestReleaseCleanupPreservesProtectedReleasesAndTags(testContext *testing.T) {
	draft := cleanupRelease(6, "draft")
	draft["draft"] = true
	draft["published_at"] = nil
	prerelease := cleanupRelease(4, "v0.1.0-beta.1")
	prerelease["prerelease"] = true
	result := runCleanupScript(testContext, map[string]string{"CURRENT_TAG": "v1"}, map[string]any{
		"pages": []any{
			[]any{cleanupRelease(2, "v99"), draft, cleanupRelease(5, "v5")},
			[]any{cleanupRelease(1, "v1"), prerelease, cleanupRelease(3, "v3")},
		},
		"latest": cleanupRelease(3, "v3"),
	})
	if len(result.Failures) != 0 || !reflect.DeepEqual(cleanupDeletedIDs(result), []int{2}) || result.Outputs["deleted-count"] != "1" {
		testContext.Fatalf("result=%+v", result)
	}
	seenDeletion := false
	for _, call := range result.Calls {
		if call.Method == "delete" {
			seenDeletion = true
		}
		if seenDeletion && call.Method == "list" {
			testContext.Fatal("pagination continued after deletion started")
		}
	}
}

func TestReleaseCleanupRetains1000PublishedVersions(testContext *testing.T) {
	var pages []any
	for first := 1; first <= 1002; first += 100 {
		var page []any
		for identifier := first; identifier < first+100 && identifier <= 1002; identifier++ {
			page = append(page, cleanupRelease(identifier, fmt.Sprintf("v%d", identifier)))
		}
		pages = append(pages, page)
	}
	result := runCleanupScript(testContext, map[string]string{"KEEP_LATEST": "1000", "CURRENT_TAG": "v1002"}, map[string]any{
		"pages": pages, "latest": cleanupRelease(1002, "v1002"),
	})
	if len(result.Failures) != 0 || !reflect.DeepEqual(cleanupDeletedIDs(result), []int{2, 1}) {
		testContext.Fatalf("result=%+v", result)
	}
}

func TestReleaseCleanupDisabledAndReadOnlyEventsMakeNoRequests(testContext *testing.T) {
	for _, test := range []struct {
		name, event string
		environment map[string]string
	}{
		{name: "disabled", environment: map[string]string{"CLEANUP_ENABLED": "false"}},
		{name: "dry run", environment: map[string]string{"DRY_RUN": "true"}},
		{name: "pull request", event: "pull_request"},
		{name: "pull request target", event: "pull_request_target"},
	} {
		testContext.Run(test.name, func(testContext *testing.T) {
			result := runCleanupScript(testContext, test.environment, map[string]any{"event": test.event, "pages": []any{[]any{cleanupRelease(5, "v5")}}})
			if len(result.Calls) != 0 || len(result.Failures) != 0 {
				testContext.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestReleaseCleanupFailsClosed(testContext *testing.T) {
	for _, test := range []struct {
		name        string
		environment map[string]string
		state       map[string]any
	}{
		{name: "invalid count", environment: map[string]string{"KEEP_LATEST": "0"}},
		{name: "fractional count", environment: map[string]string{"KEEP_LATEST": "1.5"}},
		{name: "unsafe count", environment: map[string]string{"KEEP_LATEST": "9007199254740992"}},
		{name: "missing current", environment: map[string]string{"CURRENT_TAG": "missing"}},
		{name: "invalid timestamp", state: map[string]any{"pages": []any{[]any{map[string]any{"id": 1, "tag_name": "v1", "published_at": "invalid"}, cleanupRelease(5, "v5")}}}},
		{name: "pagination failure", state: map[string]any{"listStatus": 403, "failPage": 1}},
		{name: "latest lookup failure", state: map[string]any{"latestStatus": 403}},
	} {
		testContext.Run(test.name, func(testContext *testing.T) {
			state := map[string]any{"pages": []any{[]any{cleanupRelease(1, "v1"), cleanupRelease(5, "v5")}, []any{cleanupRelease(2, "v2")}}}
			for key, value := range test.state {
				state[key] = value
			}
			result := runCleanupScript(testContext, test.environment, state)
			if len(result.Failures) != 1 || len(cleanupDeletedIDs(result)) != 0 || strings.Contains(strings.Join(result.Failures, ""), "secret response") {
				testContext.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestReleaseCleanupWorkflowGates(testContext *testing.T) {
	step := licenseWorkflowStep(testContext, "Clean up old GitHub Releases")
	condition := step["if"].(string)
	for _, required := range []string{"success()", "release-cleanup-enabled", "!inputs.dry-run", "pull_request", "official-review-pending", "asset-url.outputs.download-url", "existing-asset.outputs.download-url", "steps.publish-official.outcome != 'failure'"} {
		if !strings.Contains(condition, required) {
			testContext.Fatalf("cleanup condition missing %q: %s", required, condition)
		}
	}
	with := step["with"].(map[string]any)
	if !strings.Contains(with["github-token"].(string), "secrets.GH_RELEASE_TOKEN || github.token") || step["continue-on-error"] != true {
		testContext.Fatalf("step=%+v", step)
	}
}

func TestReleaseCleanupRetriesAndDeletedReleases(testContext *testing.T) {
	for _, test := range []struct {
		name, wantCount string
		state           map[string]any
		wantFailure     bool
		wantDeletes     int
	}{
		{name: "deduplicated release IDs", wantCount: "3", wantDeletes: 3},
		{name: "no stable latest release", state: map[string]any{"latestStatus": 404}, wantCount: "3", wantDeletes: 3},
		{name: "already deleted", state: map[string]any{"deleteStatus": 404}, wantCount: "0", wantDeletes: 3},
		{name: "delete denied stops immediately", state: map[string]any{"deleteStatus": 403}, wantCount: "0", wantFailure: true, wantDeletes: 1},
	} {
		testContext.Run(test.name, func(testContext *testing.T) {
			state := map[string]any{"pages": []any{
				[]any{cleanupRelease(1, "v1"), cleanupRelease(3, "v3"), cleanupRelease(5, "v5")},
				[]any{cleanupRelease(2, "v2"), cleanupRelease(3, "v3"), cleanupRelease(4, "v4")},
			}}
			for key, value := range test.state {
				state[key] = value
			}
			result := runCleanupScript(testContext, nil, state)
			if (len(result.Failures) != 0) != test.wantFailure || result.Outputs["deleted-count"] != test.wantCount || len(cleanupDeletedIDs(result)) != test.wantDeletes {
				testContext.Fatalf("result=%+v", result)
			}
		})
	}
}
