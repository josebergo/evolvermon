package api

// 附件（A13，总册 7.3.1）：multipart 上传到本地数据目录，按对象关联；下载经 API 鉴权。

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type AttachmentAPI struct{ DB *gorm.DB }

func dataDir() string {
	d := os.Getenv("EM_DATA_DIR")
	if d == "" {
		d = "data"
	}
	return d
}

// Upload POST /attachments（form: file, objectType, objectId）
func (a *AttachmentAPI) Upload(c *gin.Context) {
	ac := middleware.GetAuth(c)
	objType := c.PostForm("objectType")
	objID := c.PostForm("objectId")
	if objType == "" || objID == "0" || objID == "" {
		response.Fail(c, response.NewBiz(response.ErrParam, "objectType/objectId 必填"))
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "file 必传"))
		return
	}
	if fh.Size > 20<<20 {
		response.Fail(c, response.NewBiz(response.ErrParam, "附件不可超过 20MB"))
		return
	}
	dir := filepath.Join(dataDir(), "attachments", fmt.Sprint(ac.TenantID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		response.Fail(c, err)
		return
	}
	att := model.Attachment{
		TenantID: ac.TenantID, ObjectType: objType,
		FileName: filepath.Base(fh.Filename), UploadedBy: ac.UserID,
	}
	var objID64 int64
	fmt.Sscanf(objID, "%d", &objID64)
	att.ObjectID = objID64
	if err := a.DB.Create(&att).Error; err != nil {
		response.Fail(c, err)
		return
	}
	stored := filepath.Join(dir, fmt.Sprintf("%d_%s", att.ID, att.FileName))
	if err := c.SaveUploadedFile(fh, stored); err != nil {
		a.DB.Delete(&att)
		response.Fail(c, err)
		return
	}
	att.StoredPath = stored
	att.Size = fh.Size
	a.DB.Model(&model.Attachment{}).Where("id=?", att.ID).Updates(map[string]any{"stored_path": stored, "size": fh.Size})
	att.StoredPath = ""
	response.OK2(c, att)
}

// List GET /attachments?objectType=&objectId=
func (a *AttachmentAPI) List(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var list []model.Attachment
	a.DB.Where("tenant_id=? AND object_type=? AND object_id=?", ac.TenantID, c.Query("objectType"), c.Query("objectId")).
		Order("id DESC").Limit(100).Find(&list)
	response.OK2(c, list)
}

// Download GET /attachments/:id/download
func (a *AttachmentAPI) Download(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var att model.Attachment
	if err := a.DB.Where("id=? AND tenant_id=?", c.Param("id"), ac.TenantID).First(&att).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "附件不存在"))
		return
	}
	if att.StoredPath == "" {
		response.Fail(c, response.NewBiz(response.ErrBiz, "附件文件缺失"))
		return
	}
	c.FileAttachment(att.StoredPath, att.FileName)
}

var _ = io.Discard
