package middleware

import "github.com/gin-gonic/gin"

func MetadataHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Powered-By", "FreeModelGateway")
		c.Next()
	}
}
