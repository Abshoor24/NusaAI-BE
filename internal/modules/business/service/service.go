package service

import (
	"log/slog"

	"github.com/Abshoor24/backend-go/internal/modules/business/repository"
)

// Service is the business logic contract used by HTTP handlers. It depends on
// the repository interface so tests can substitute a mock.
//
// TODO: CLIENT REQUIREMENT - define the interface methods once the client's
// business rules are known, e.g.:
//
//	List(ctx context.Context, req model.ListRequest) (model.ListResponse, error)
type Service interface{}

type service struct {
	repo   repository.BusinessRepository
	logger *slog.Logger
}

func New(repo repository.BusinessRepository, logger *slog.Logger) Service {
	return &service{repo: repo, logger: logger}
}

// TODO: CLIENT REQUIREMENT - implement the business use cases below once the
// requirements are gathered, e.g.:
//
// func (s *service) List(ctx context.Context, req model.ListRequest) (model.ListResponse, error) {
// 	...
// }