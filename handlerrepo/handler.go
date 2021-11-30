package handlerrepo

import (
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/utils/handler"
)

// AddHandler is an interface for separate states to bind their handlers.
type AddHandler interface {
	AddHandler(fn interface{}) (cancel func())
	AddSyncHandler(fn interface{}) (cancel func())
}

var _ AddHandler = (*handler.Handler)(nil)

// Unbinder is an interface for separate states to remove their handlers.
type Unbinder interface {
	Unbind()
}

type Repository struct {
	adder  AddHandler
	cancel []func()
}

func NewRepository(adder AddHandler) *Repository {
	return &Repository{
		adder: adder,
	}
}

func (r *Repository) AddHandler(fn interface{}) (cancel func()) {
	cancel = r.adder.AddHandler(fn)
	r.cancel = append(r.cancel, cancel)
	return
}

func (r *Repository) AddSyncHandler(fn interface{}) (cancel func()) {
	cancel = r.adder.AddSyncHandler(fn)
	r.cancel = append(r.cancel, cancel)
	return
}

func (r *Repository) Unbind() {
	for _, fn := range r.cancel {
		fn()
	}
}

// ReadyInjector is an event handler wrapper that allows injecting the Ready
// event.
type ReadyInjector struct {
	adder AddHandler
	ready *gateway.ReadyEvent
}

func NewReadyInjector(adder AddHandler, r *gateway.ReadyEvent) *ReadyInjector {
	return &ReadyInjector{
		adder: adder,
		ready: r,
	}
}

func (r *ReadyInjector) AddHandler(fn interface{}) (cancel func()) {
	if readyfn, ok := fn.(func(*gateway.ReadyEvent)); ok {
		readyfn(r.ready)
	}

	cancel = r.adder.AddHandler(fn)
	return
}
