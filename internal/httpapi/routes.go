package httpapi

import "github.com/go-chi/chi/v5"

// Route groups under /api. They are mounted now and filled by later tasks
// (setup wizard, login flow, admin settings, app tiles).

func (s *server) setupRoutes(r chi.Router) {}

func (s *server) authRoutes(r chi.Router) {}

func (s *server) adminRoutes(r chi.Router) {}

func (s *server) appRoutes(r chi.Router) {}
