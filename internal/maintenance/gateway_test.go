package maintenance

import (
	"context"
	"reflect"
	"testing"
)

type capturedCall struct {
	directory  string
	executable string
	arguments  []string
}

type captureRunner struct {
	response []byte
	calls    []capturedCall
}

func (runner *captureRunner) Run(_ context.Context, directory, executable string, arguments ...string) ([]byte, error) {
	runner.calls = append(runner.calls, capturedCall{directory: directory, executable: executable, arguments: append([]string(nil), arguments...)})
	return runner.response, nil
}

func TestPullRequestGatewayUsesExactRepositoryAndBranchArguments(t *testing.T) {
	runner := &captureRunner{response: []byte("[]")}
	repository := Repository{TopLevel: "/workspace/api", RemoteSlug: "example/repository"}
	if _, err := pullRequests(context.Background(), runner, repository, "GH-20"); err != nil {
		t.Fatal(err)
	}
	want := capturedCall{
		directory: "/workspace/api", executable: "gh",
		arguments: []string{"pr", "list", "--repo", "example/repository", "--state", "all", "--limit", "100", "--head", "GH-20", "--json", "number,state,mergedAt,baseRefName,headRefName,headRefOid,isCrossRepository,url"},
	}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("gateway call = %#v, want %#v", runner.calls, want)
	}
}

func TestProcessGatewayUsesBoundedCWDListingArguments(t *testing.T) {
	runner := &captureRunner{response: []byte("p123\nfcwd\nn/workspace/api/GH-20\n")}
	processes, err := worktreeProcesses(context.Background(), runner, "/workspace/api", "/workspace/api/GH-20")
	if err != nil {
		t.Fatal(err)
	}
	want := capturedCall{directory: "/workspace/api", executable: "lsof", arguments: []string{"-a", "-d", "cwd", "-Fn"}}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], want) || !reflect.DeepEqual(processes, []string{"123"}) {
		t.Fatalf("gateway call = %#v, processes = %#v", runner.calls, processes)
	}
}
