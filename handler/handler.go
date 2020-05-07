package handler

// AddHandler is an interface for separate states to bind their handlers.
type AddHandler interface {
	AddHandler(fn interface{}) (cancel func())
}

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

func (r *Repository) AddHandler(fn interface{}) {
	cancel := r.adder.AddHandler(fn)
	r.cancel = append(r.cancel, cancel)
}

func (r *Repository) Unbind() {
	for _, fn := range r.cancel {
		fn()
	}
}
