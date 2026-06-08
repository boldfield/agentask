package tuiclient

import (
	"context"
)

// MockClient is a mock implementation of the Client interface for testing.
type MockClient struct {
	ListProjectsFunc   func(ctx context.Context) ([]Project, error)
	ListTasksFunc      func(ctx context.Context, projectID string) ([]Task, error)
	GetTaskFunc        func(ctx context.Context, id string) (TaskDetail, error)
	ListEventsFunc     func(ctx context.Context, taskID string) ([]Event, error)
	ListDocumentsFunc  func(ctx context.Context, projectID string) ([]Document, error)
	PromoteTaskFunc    func(ctx context.Context, id string) error
	ReviewTaskFunc     func(ctx context.Context, id, actor, verdict string, note *string) error
	TransitionTaskFunc func(ctx context.Context, id, to string, note *string) error
	ArchiveTaskFunc    func(ctx context.Context, id string) error
	ArchiveProjectFunc func(ctx context.Context, id string) error
	Tasks              []Task // for simple test data
}

func (m *MockClient) ListProjects(ctx context.Context) ([]Project, error) {
	return m.ListProjectsFunc(ctx)
}

func (m *MockClient) ListTasks(ctx context.Context, projectID string) ([]Task, error) {
	if m.ListTasksFunc != nil {
		return m.ListTasksFunc(ctx, projectID)
	}
	return m.Tasks, nil
}

func (m *MockClient) GetTask(ctx context.Context, id string) (TaskDetail, error) {
	if m.GetTaskFunc != nil {
		return m.GetTaskFunc(ctx, id)
	}
	return TaskDetail{}, nil
}

func (m *MockClient) ListEvents(ctx context.Context, taskID string) ([]Event, error) {
	if m.ListEventsFunc != nil {
		return m.ListEventsFunc(ctx, taskID)
	}
	return nil, nil
}

func (m *MockClient) ListDocuments(ctx context.Context, projectID string) ([]Document, error) {
	if m.ListDocumentsFunc != nil {
		return m.ListDocumentsFunc(ctx, projectID)
	}
	return nil, nil
}

func (m *MockClient) PromoteTask(ctx context.Context, id string) error {
	return m.PromoteTaskFunc(ctx, id)
}

func (m *MockClient) ReviewTask(ctx context.Context, id, actor, verdict string, note *string) error {
	return m.ReviewTaskFunc(ctx, id, actor, verdict, note)
}

func (m *MockClient) TransitionTask(ctx context.Context, id, to string, note *string) error {
	return m.TransitionTaskFunc(ctx, id, to, note)
}

func (m *MockClient) ArchiveTask(ctx context.Context, id string) error {
	if m.ArchiveTaskFunc != nil {
		return m.ArchiveTaskFunc(ctx, id)
	}
	return nil
}

func (m *MockClient) ArchiveProject(ctx context.Context, id string) error {
	if m.ArchiveProjectFunc != nil {
		return m.ArchiveProjectFunc(ctx, id)
	}
	return nil
}
