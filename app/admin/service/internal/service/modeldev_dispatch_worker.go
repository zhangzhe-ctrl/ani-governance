package service

import (
	"context"
	"errors"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
)

// ModelDevDispatchWorker delivers already committed CPU admissions. It owns
// neither current user authorization nor Release/default resolution.
type ModelDevDispatchWorker struct {
	repository *data.ModelDevAcceptanceRepo
	client *data.ModelDevClient
}

func NewModelDevDispatchWorker(_ *bootstrap.Context, repository *data.ModelDevAcceptanceRepo, client *data.ModelDevClient) *ModelDevDispatchWorker {
	return &ModelDevDispatchWorker{repository: repository, client: client}
}

func (w *ModelDevDispatchWorker) Start(context.Context) error {
	return errors.New("modeldev dispatch worker not implemented")
}

// The RED stub has not started a goroutine or acquired a delivery lease.
func (w *ModelDevDispatchWorker) Stop(context.Context) error {
	return nil
}
