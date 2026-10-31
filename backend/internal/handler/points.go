package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var mockPoints = 52

func GetPointsBalance(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"balance": mockPoints,
		},
	})
}
