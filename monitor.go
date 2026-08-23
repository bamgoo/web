package web

import (
	"github.com/infrago/base"
	"github.com/infrago/infra"
)

func (m *Module) Ready() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.started && m.instance != nil && m.instance.connect != nil
}

func (m *Module) Health() infra.ModuleHealth {
	m.mutex.Lock()
	started := m.started
	connected := m.instance != nil && m.instance.connect != nil
	sites := len(m.sites)
	m.mutex.Unlock()
	return infra.NewModuleHealth("web", started && connected, nil, base.Map{
		"connected": connected,
		"sites":     sites,
	})
}

func (m *Module) Stats() infra.ModuleStats {
	m.mutex.Lock()
	started := m.started
	connected := m.instance != nil && m.instance.connect != nil
	sites := len(m.sites)
	routers := 0
	for _, site := range m.sites {
		routers += len(site.routers)
	}
	m.mutex.Unlock()
	return infra.NewModuleStats("web", started && connected, base.Map{
		"connected": connected,
		"sites":     sites,
		"routers":   routers,
	})
}
