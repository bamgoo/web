package web

import (
	"net/http"

	. "github.com/bamgoo/base"
)

type (
	// Driver defines web driver interface.
	Driver interface {
		Connect(*Instance) (Connection, error)
	}

	// Connection defines web connection interface.
	Connection interface {
		Open() error
		Close() error

		Register(name string, info Info, domains []string, domain string) error

		Start() error
		StartTLS(certFile, keyFile string) error
	}

	// Delegate handles web requests.
	Delegate interface {
		Serve(name string, params Map, res http.ResponseWriter, req *http.Request)
	}

	// Info contains route information.
	Info struct {
		Method string
		Uri    string
		Router string
		Args   Vars
	}
)
