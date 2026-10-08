package metadata

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func licenseWorkflowStep(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []map[string]any `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["lazycat"].Steps {
		if step["name"] == name {
			return step
		}
	}
	t.Fatalf("step %q missing", name)
	return nil
}

func gitLicense(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func TestLicenseCommitIsSeparateFromVersionAndManifest(t *testing.T) {
	for _, versionUpdate := range []bool{false, true} {
		name := "license only"
		if versionUpdate {
			name = "license and version"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			remote := filepath.Join(t.TempDir(), "remote.git")
			gitLicense(t, root, "init", "--bare", remote)
			gitLicense(t, root, "init", "-b", "main")
			gitLicense(t, root, "config", "user.name", "test")
			gitLicense(t, root, "config", "user.email", "test@example.com")
			original := "package: example\nversion: 1.0.0\nlicense: Apache-2.0\n"
			pkg := filepath.Join(root, "package.yml")
			manifest := filepath.Join(root, "lzc-manifest.yml")
			if err := os.WriteFile(pkg, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, []byte("image: old\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitLicense(t, root, "add", "package.yml", "lzc-manifest.yml")
			gitLicense(t, root, "commit", "-m", "initial")
			gitLicense(t, root, "remote", "add", "origin", remote)
			gitLicense(t, root, "push", "-u", "origin", "main")
			snapshot := filepath.Join(t.TempDir(), "license.yml")
			licenseOnly := strings.ReplaceAll(original, "Apache-2.0", "MIT")
			if err := os.WriteFile(snapshot, []byte(licenseOnly), 0o600); err != nil {
				t.Fatal(err)
			}
			final := licenseOnly
			version := "1.0.0"
			if versionUpdate {
				version = "1.1.0"
				final = strings.ReplaceAll(final, "1.0.0", version)
				if err := os.WriteFile(manifest, []byte("image: new\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(pkg, []byte(final), 0o644); err != nil {
				t.Fatal(err)
			}
			run := func(step string) {
				script := licenseWorkflowStep(t, step)["run"].(string)
				command := exec.Command("bash", "-c", script)
				command.Dir = root
				command.Env = append(os.Environ(), "GITHUB_WORKSPACE="+root, "GITHUB_REF_NAME=main", "GITHUB_OUTPUT="+filepath.Join(root, "output"), "PACKAGE_FILE="+pkg, "MANIFEST_FILE="+manifest, "LICENSE_FILE="+snapshot, "LICENSE=MIT", "VERSION="+version)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", step, err, output)
				}
			}
			run("Commit license synchronization separately")
			if got := gitLicense(t, root, "show", "HEAD:package.yml"); got != licenseOnly {
				t.Fatalf("license commit=%s", got)
			}
			if got := gitLicense(t, root, "show", "HEAD:lzc-manifest.yml"); got != "image: old\n" {
				t.Fatalf("manifest leaked into license commit: %s", got)
			}
			run("Commit direct-publish update")
			wantCount := "2"
			if versionUpdate {
				wantCount = "3"
			}
			if got := strings.TrimSpace(gitLicense(t, root, "rev-list", "--count", "HEAD")); got != wantCount {
				t.Fatalf("commit count=%s want %s", got, wantCount)
			}
			if got := gitLicense(t, root, "show", "HEAD:package.yml"); got != final {
				t.Fatalf("final commit=%s", got)
			}
			if got := gitLicense(t, root, "rev-parse", "HEAD"); got != gitLicense(t, root, "rev-parse", "origin/main") {
				t.Fatal("commits were not pushed")
			}
		})
	}
}

func TestTagSyncUsesIndependentLicenseCommit(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "package.yml")
	final := "package: example\nversion: 1.1.0\nlicense: MIT # retain comment\ndescription: |\n  Hello\n"
	original := strings.ReplaceAll(strings.ReplaceAll(final, "1.1.0", "1.0.0"), "MIT", "Apache-2.0")
	if err := os.WriteFile(pkg, []byte(final), 0o644); err != nil {
		t.Fatal(err)
	}
	with := licenseWorkflowStep(t, "Sync tag/release version to default branch")["with"].(map[string]any)
	script := with["script"].(string)
	originalJSON, _ := json.Marshal(original)
	wrapper := `const updates=[]; const context={repo:{owner:'o',repo:'r'}};
 const core={setFailed:msg=>{throw new Error(msg)}};
 const github={rest:{repos:{get:async()=>({data:{default_branch:'main'}}),getContent:async()=>({data:{type:'file',sha:'original',content:Buffer.from(` + string(originalJSON) + `).toString('base64')}}),createOrUpdateFileContents:async req=>{updates.push(req);return {data:{content:{sha:'license-sha'}}}}}}};
 (async()=>{` + script + `;console.log(JSON.stringify(updates))})().catch(err=>{console.error(err);process.exit(1)});`
	command := exec.Command("node", "-e", wrapper)
	command.Env = append(os.Environ(), "GITHUB_WORKSPACE="+root, "PACKAGE_FILE="+pkg, "VERSION=1.1.0", "LICENSE_CHANGED=true", "LICENSE=MIT")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("tag sync: %v\n%s", err, output)
	}
	var updates []struct {
		Content string `json:"content"`
		Message string `json:"message"`
		SHA     string `json:"sha"`
	}
	if err := json.Unmarshal(output, &updates); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 || !strings.Contains(updates[0].Message, "license") || !strings.Contains(updates[1].Message, "version") || updates[1].SHA != "license-sha" {
		t.Fatalf("updates=%+v", updates)
	}
	// Decode with YAML as well as comparing bytes: the first commit only changes license.
	for i, update := range updates {
		data, err := base64.StdEncoding.DecodeString(update.Content)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.ReplaceAll(original, "Apache-2.0", "MIT")
		if i == 1 {
			want = final
		}
		if string(data) != want {
			t.Fatalf("commit %d changed extra fields:\n%s", i, data)
		}
		var document map[string]any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		if document["license"] != "MIT" {
			t.Fatalf("license=%v", document["license"])
		}
	}
}
