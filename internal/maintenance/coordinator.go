package maintenance

import "context"

type ResumeFunc func(context.Context) error

type Coordinator interface {
	AffectedServices(ctx context.Context, worktreePath string) ([]string, error)
	Pause(ctx context.Context, worktreePath string) ([]string, ResumeFunc, error)
}

type NoopCoordinator struct{}

func (NoopCoordinator) AffectedServices(context.Context, string) ([]string, error) {
	return nil, nil
}

func (NoopCoordinator) Pause(context.Context, string) ([]string, ResumeFunc, error) {
	return nil, func(context.Context) error { return nil }, nil
}
