package tuiclient

import (
	"context"
)

// MockClient is a mock implementation of the Client interface for testing.
type MockClient struct {
	ListProjectsFunc   func(ctx context.Context) ([]Project, error)
	ListTasksFunc      func(ctx context.Context, projectID string) ([]Task, error)
	GetTaskFunc        func(ctx context.Context, id string) (TaskDetail, error)
	ListDocumentsFunc  func(ctx context.Context, projectID string) ([]Document, error)
	PromoteTaskFunc    func(ctx context.Context, id string) error
	ReviewTaskFunc     func(ctx context.Context, id, actor, verdict string, note *string) error
	TransitionTaskFunc func(ctx context.Context, id, to string, note *string) error
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
	return m.GetTaskFunc(ctx, id)
}

func (m *MockClient) ListDocuments(ctx context.Context, projectID string) ([]Document, error) {
	return m.ListDocumentsFunc(ctx, projectID)
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

// MockClientFunc provides function-based methods for testing.
type MockClientFunc struct {
	ListProjectsFn   func(ctx context.Context) ([]Project, error)
	ListTasksFn      func(ctx context.Context, projectID string) ([]Task, error)
	GetTaskFn        func(ctx context.Context, id string) (TaskDetail, error)
	ListDocumentsFn  func(ctx context.Context, projectID string) ([]Document, error)
	PromoteTaskFn    func(ctx context.Context, id string) error
	ReviewTaskFn     func(ctx context.Context, id, actor, verdict string, note *string) error
	TransitionTaskFn func(ctx context.Context, id, to string, note *string) error
}

func (m *MockClientFunc) ListProjects(ctx context.Context) ([]Project, error) {
	return m.ListProjectsFn(ctx)
}

func (m *MockClientFunc) ListTasks(ctx context.Context, projectID string) ([]Task, error) {
	return m.ListTasksFn(ctx, projectID)
}

func (m *MockClientFunc) GetTask(ctx context.Context, id string) (TaskDetail, error) {
	return m.GetTaskFn(ctx, id)
}

func (m *MockClientFunc) ListDocuments(ctx context.Context, projectID string) ([]Document, error) {
	return m.ListDocumentsFn(ctx, projectID)
}

func (m *MockClientFunc) PromoteTask(ctx context.Context, id string) error {
	return m.PromoteTaskFn(ctx, id)
}

func (m *MockClientFunc) ReviewTask(ctx context.Context, id, actor, verdict string, note *string) error {
	return m.ReviewTaskFn(ctx, id, actor, verdict, note)
}

func (m *MockClientFunc) TransitionTask(ctx context.Context, id, to string, note *string) error {
	return m.TransitionTaskFn(ctx, id, to, note)
}
