package httpapi

import "github.com/go-chi/chi/v5"

// Route groups under /api. They are mounted now and filled by later tasks
// (admin settings, app tiles).

func (s *server) adminRoutes(r chi.Router) {}

func (s *server) appRoutes(r chi.Router) {}
