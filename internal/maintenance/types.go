package maintenance

import "time"

const (
	OperationSync  = "sync"
	OperationPrune = "worktrees-prune"
)

type Options struct {
	Repositories []string
	DryRun       bool
}

type Repository struct {
	Name          string
	Aliases       []string
	Path          string
	TopLevel      string
	CommonDir     string
	Primary       string
	Remote        string
	DefaultBranch string
	RemoteSlug    string
	DeclaredPaths []string
}

type SyncReport struct {
	Operation    string           `json:"operation"`
	DryRun       bool             `json:"dry_run"`
	StartedAt    string           `json:"started_at"`
	FinishedAt   string           `json:"finished_at"`
	Repositories []SyncRepository `json:"repositories"`
	Failures     []Failure        `json:"failures"`
}

type SyncRepository struct {
	Name          string   `json:"name"`
	Aliases       []string `json:"aliases,omitempty"`
	Remote        string   `json:"remote"`
	DefaultBranch string   `json:"default_branch,omitempty"`
	LocalOID      string   `json:"local_oid,omitempty"`
	RemoteOID     string   `json:"remote_oid,omitempty"`
	Path          string   `json:"path,omitempty"`
	State         string   `json:"state"`
	Action        string   `json:"action"`
	Detail        string   `json:"detail,omitempty"`
	Services      []string `json:"services,omitempty"`
}

type PruneReport struct {
	Operation    string        `json:"operation"`
	DryRun       bool          `json:"dry_run"`
	StartedAt    string        `json:"started_at"`
	FinishedAt   string        `json:"finished_at"`
	Repositories []PruneResult `json:"repositories"`
	Failures     []Failure     `json:"failures"`
}

type PruneResult struct {
	Name          string             `json:"name"`
	Aliases       []string           `json:"aliases,omitempty"`
	Remote        string             `json:"remote"`
	DefaultBranch string             `json:"default_branch,omitempty"`
	Worktrees     []WorktreeDecision `json:"worktrees"`
}

type WorktreeDecision struct {
	Repository  string       `json:"repository"`
	Path        string       `json:"path"`
	Branch      string       `json:"branch,omitempty"`
	HeadOID     string       `json:"head_oid,omitempty"`
	Action      string       `json:"action"`
	Reason      string       `json:"reason"`
	Detail      string       `json:"detail,omitempty"`
	PullRequest *PullRequest `json:"pull_request,omitempty"`
	SafeLinks   []string     `json:"-"`
}

type PullRequest struct {
	Number            int        `json:"number"`
	State             string     `json:"state"`
	MergedAt          *time.Time `json:"mergedAt"`
	BaseRefName       string     `json:"baseRefName"`
	HeadRefName       string     `json:"headRefName"`
	HeadRefOID        string     `json:"headRefOid"`
	IsCrossRepository bool       `json:"isCrossRepository"`
	URL               string     `json:"url"`
}

type Failure struct {
	Repository string `json:"repository,omitempty"`
	Operation  string `json:"operation"`
	Path       string `json:"path,omitempty"`
	Error      string `json:"error"`
}

type worktree struct {
	Path     string
	Head     string
	Branch   string
	Detached bool
	Locked   bool
	Prunable bool
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
