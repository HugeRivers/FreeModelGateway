package page

import (
	"path/filepath"

	"github.com/gin-gonic/gin"
)

type SPAHandler struct {
	webAppPath string
}

func NewSPA(webAppPath string) *SPAHandler {
	return &SPAHandler{webAppPath: webAppPath}
}

func (s *SPAHandler) Index(c *gin.Context) {
	c.File(filepath.Join(s.webAppPath, "index.html"))
}
