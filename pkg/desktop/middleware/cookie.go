package middleware

import (
	"github.com/gin-gonic/gin"

	"xpam/secureconnection/pkg/auth"
	"xpam/secureconnection/pkg/config"

	"xpam/sdk-go/service"
)

func JmsCookieAuth(jmsService *service.JMService) gin.HandlerFunc {
	authenticate := auth.HTTPMiddleSessionAuth(jmsService)
	return func(ctx *gin.Context) {
		authenticate(ctx)
		if ctx.IsAborted() {
			return
		}
		if user, ok := ctx.Get(auth.ContextKeyUser); ok {
			ctx.Set(config.GinCtxUserKey, user)
		}
	}
}
