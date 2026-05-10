package agentcli

import "testing"

func TestNormalizeRunnerPermissionRequestRejectsInvalidTargets(t *testing.T) {
	for _, target := range []string{"", ".", "/"} {
		if _, ok := NormalizeRunnerPermissionRequest(RunnerPermissionRequest{RunnerID: string(RunnerCodex), TargetPath: target}); ok {
			t.Fatalf("expected target %q to be rejected", target)
		}
	}
}

func TestRunnerPermissionFromMetaBranches(t *testing.T) {
	type permissionAlias struct {
		RunnerID   string `json:"runner_id"`
		TargetPath string `json:"target_path"`
	}
	cases := []struct {
		name string
		meta map[string]any
		want RunnerPermissionRequest
		ok   bool
	}{
		{name: "nil meta", meta: nil, ok: false},
		{name: "missing key", meta: map[string]any{"other": true}, ok: false},
		{name: "request struct", meta: map[string]any{"runner_permission": RunnerPermissionRequest{RunnerID: string(RunnerCodex), Access: runnerPermissionAccessWrite, TargetPath: "/Users/test/file"}}, want: RunnerPermissionRequest{RunnerID: string(RunnerCodex), Kind: runnerPermissionKindFilesystem, Access: runnerPermissionAccessWrite, TargetPath: "/Users/test/file"}, ok: true},
		{name: "map input", meta: map[string]any{"runner_permission": map[string]any{"runner_id": string(RunnerOpenCode), "targetPath": "/Users/test/project"}}, want: RunnerPermissionRequest{RunnerID: string(RunnerOpenCode), Kind: runnerPermissionKindFilesystem, Access: runnerPermissionAccessRead, TargetPath: "/Users/test/project"}, ok: true},
		{name: "json marshaled struct", meta: map[string]any{"runner_permission": permissionAlias{RunnerID: string(RunnerGemini), TargetPath: "/Users/test/work"}}, want: RunnerPermissionRequest{RunnerID: string(RunnerGemini), Kind: runnerPermissionKindFilesystem, Access: runnerPermissionAccessRead, TargetPath: "/Users/test/work"}, ok: true},
		{name: "unmarshal failure", meta: map[string]any{"runner_permission": 42}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := runnerPermissionFromMeta(tc.meta)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got %v (%#v)", tc.ok, ok, got)
			}
			if tc.ok && got != tc.want {
				t.Fatalf("expected %#v, got %#v", tc.want, got)
			}
		})
	}
}
