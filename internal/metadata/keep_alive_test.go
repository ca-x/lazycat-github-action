package metadata

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRepositoryActivityStep(t *testing.T) {
	step := licenseWorkflowStep(t, "Record repository activity")
	condition := step["if"].(string)
	for _, guard := range []string{"!cancelled()", "inputs.keep-alive", "!inputs.dry-run"} {
		if !strings.Contains(condition, guard) {
			t.Fatalf("activity step is missing %q", guard)
		}
	}
	with := step["with"].(map[string]any)
	if with["github-token"] != "${{ github.token }}" {
		t.Fatal("activity step must use the caller's workflow token")
	}

	wrapper := `const assert = require('node:assert/strict');
const scenario = process.argv[1];
const repository = {owner: 'caller', repo: 'source'};
const context = {
  repo: repository, runId: 123,
  eventName: scenario === 'tag' ? 'push' : 'workflow_dispatch',
  ref: scenario === 'tag' ? 'refs/tags/v2' :
    scenario === 'other branch' ? 'refs/heads/topic' : 'refs/heads/trunk'
};
const calls = [];
const warnings = [];
const core = {warning: message => warnings.push(message)};
const failure = status => Object.assign(new Error('private response'), {status});
const file = {...repository, path: '.github/lazycat-keep-alive.txt'};
const github = {rest: {repos: {
  get: async options => {
    assert.deepEqual(options, repository);
    return {data: {default_branch: 'trunk'}};
  },
  getContent: async options => {
    assert.deepEqual(options, {...file, ref: 'trunk'});
    if (scenario === 'first run') throw failure(404);
    return {data: {type: 'file', sha: calls.length ? 'current' : 'initial'}};
  },
  createOrUpdateFileContents: async options => {
    calls.push(options);
    if (scenario === 'conflict' && calls.length === 1) throw failure(409);
    if (scenario === 'no permission') throw failure(403);
  }
}}};
const execute = async () => {` + with["script"].(string) + `};
(async () => {
  await execute();
  const expected = scenario === 'other branch' ? [] :
    scenario === 'first run' ? [undefined] :
    scenario === 'conflict' ? ['initial', 'current'] : ['initial'];
  assert.deepEqual(calls.map(call => call.sha), expected);
  for (const {content, message, sha, ...target} of calls) {
    assert.deepEqual(target, {...file, branch: 'trunk'});
    assert.match(message, /\[skip ci\]/);
    assert.match(Buffer.from(content, 'base64').toString(), /^Updated: \d{4}-.*Z\nRun: 123\nAttempt: 2\n$/);
  }
  assert.equal(warnings.length, scenario === 'no permission' ? 1 : 0);
  if (warnings.length) {
    assert.match(warnings[0], /HTTP 403/);
    assert.doesNotMatch(warnings[0], /private response/);
  }
})().catch(error => { console.error(error); process.exitCode = 1; });`
	for _, scenario := range []string{"first run", "existing file", "conflict", "no permission", "tag", "other branch"} {
		t.Run(scenario, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), "node", "-e", wrapper, scenario)
			command.Env = append(command.Environ(), "GITHUB_RUN_ATTEMPT=2")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("repository activity script: %v\n%s", err, output)
			}
		})
	}
}
