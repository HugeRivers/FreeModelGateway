package api

import (
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/free-model-gateway/fmg/internal/router"
	"github.com/free-model-gateway/fmg/internal/stats"
	"github.com/sirupsen/logrus"
)

var startTime = time.Now()

type Handler struct {
	router  *router.Router
	pool    *model.Pool
	stats   *stats.Collector
	log     *logrus.Logger
	version string
}

func NewHandler(r *router.Router, p *model.Pool, s *stats.Collector, log *logrus.Logger, version string) *Handler {
	return &Handler{router: r, pool: p, stats: s, log: log, version: version}
}
