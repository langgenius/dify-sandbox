package controller

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/storage"
	"github.com/langgenius/dify-sandbox/internal/types"
)

func UploadFile(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, types.ErrorResponse(400, "file is required"))
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.ErrorResponse(500, "failed to open file"))
		return
	}
	defer f.Close()

	store := storage.GetStorage()
	// Use original filename? Or just store content?
	// storage.Put takes (reader, filename).
	// The specific filename implementation in LocalStorage uses it for extension perhaps?
	// LocalStorage currently ignores the filename arg for path generation, only generating a UUID.
	// But let's pass it anyway.
	fileId, err := store.Put(f, fileHeader.Filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.ErrorResponse(500, fmt.Sprintf("failed to save file: %v", err)))
		return
	}

	c.JSON(http.StatusOK, types.SuccessResponse(gin.H{
		"file_id": fileId,
	}))
}

func DownloadFile(c *gin.Context) {
	fileId := c.Param("file_id")
	// Clean up leading slash if using wildcard
	if len(fileId) > 0 && fileId[0] == '/' {
		fileId = fileId[1:]
	}
	if fileId == "" {
		c.JSON(http.StatusBadRequest, types.ErrorResponse(400, "file_id is required"))
		return
	}

	store := storage.GetStorage()
	reader, err := store.Get(fileId)
	if err != nil {
		// Could be 404 or 500
		c.JSON(http.StatusNotFound, types.ErrorResponse(404, "file not found"))
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileId))
	c.Header("Content-Type", "application/octet-stream")

	_, err = io.Copy(c.Writer, reader)
	if err != nil {
		// Can't write JSON error if streaming started, but we can log
		// gin logs handled errors usually
		return
	}
}
