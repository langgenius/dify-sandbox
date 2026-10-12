package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func BindRequest[T any](r *gin.Context, success func(T)) {
	var request T
	var err error

	context_type := r.GetHeader("Content-Type")
	if context_type == "application/json" {
		err = r.BindJSON(&request)
	} else {
		err = r.ShouldBind(&request)
	}

	if err != nil {
		resp := types.ErrorResponse(-400, err.Error())
		r.JSON(http.StatusBadRequest, resp)
		return
	}
	success(request)
}
