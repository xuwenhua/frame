package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/internal/repository"
	"jikeai-clone/internal/service"
	"jikeai-clone/pkg/idgen"
)

type StoryboardHandler struct {
	repo        *repository.StoryboardRepository
	episodeRepo *repository.EpisodeRepository
	projectRepo *repository.ProjectRepository
	charRepo    *repository.CharacterRepository
	sceneRepo   *repository.SceneRepository
	itemRepo    *repository.ItemRepository
	pointsRepo  *repository.PointsRepository
	settingRepo *repository.SettingRepository
	provider    *service.ModelProvider
	ruleSvc     *service.StoryboardService
}

func NewStoryboardHandler(
	repo *repository.StoryboardRepository,
	episodeRepo *repository.EpisodeRepository,
	projectRepo *repository.ProjectRepository,
	charRepo *repository.CharacterRepository,
	sceneRepo *repository.SceneRepository,
	itemRepo *repository.ItemRepository,
	pointsRepo *repository.PointsRepository,
	settingRepo *repository.SettingRepository,
	provider *service.ModelProvider,
) *StoryboardHandler {
	return &StoryboardHandler{
		repo: repo, episodeRepo: episodeRepo, projectRepo: projectRepo,
		charRepo: charRepo, sceneRepo: sceneRepo, itemRepo: itemRepo,
		pointsRepo: pointsRepo, settingRepo: settingRepo, provider: provider,
		ruleSvc: service.NewStoryboardService(),
	}
}

// Generate 生成分镜（不扣积分，分镜推理算基础能力）
func (h *StoryboardHandler) Generate(c *gin.Context) {
	userID := middleware.CurrentUserID(c)

	var req struct {
		EpisodeID string `json:"episodeId" binding:"required"`
		// Duration 工具栏选的出片时长（秒）：>0 时描述词的时间轴按它生成
		Duration int `json:"duration"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}

	ep, err := h.ownedEpisode(req.EpisodeID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}
	if ep.ScriptText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "剧本为空，请先填写"})
		return
	}

	chars, _ := h.charRepo.ListByEpisodeID(req.EpisodeID)
	scenes, _ := h.sceneRepo.ListByEpisodeID(req.EpisodeID)
	items, _ := h.itemRepo.ListByEpisodeID(req.EpisodeID)

	// 优先 LLM，失败降级规则引擎
	client, cfg, modelErr := h.provider.LLM()
	if modelErr != nil {
		client = nil
	}

	svc := service.NewLLMStoryboardService(client)
	// 300 秒：LLM 带推理 + 最多 3 次重试，180 秒会被中途掐断
	ctx, cancel := context.WithTimeout(c.Request.Context(), 300*time.Second)
	defer cancel()

	drafts, llmErr := svc.GenerateFrames(ctx, ep.ScriptText, chars, scenes, items, req.Duration)
	if llmErr != nil {
		// 一定要打日志：降级到规则引擎后接口照样返回 200，
		// 不打日志就只能看到「生成成功但内容是抠句子」，根本查不出原因
		log.Printf("分镜 LLM 失败，降级规则引擎: %v", llmErr)
	}
	if len(drafts) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "剧本太短，无法拆分镜头"})
		return
	}

	// 描述词开头的「场次：1-3」要用真实集数，drafts 里默认是 0；
	// 补上之后重拼一次（纯函数，代价可忽略）
	for i := range drafts {
		drafts[i].EpisodeNo = ep.EpisodeNumber
		// 描述词按场景参考类型拼：video 模式下地点行不写家具
		drafts[i].VideoPrompt = service.BuildFrameVideoPrompt(drafts[i], drafts[i].Duration)
	}

	// 覆盖旧分镜
	_ = h.repo.DeleteByEpisodeID(req.EpisodeID)

	now := time.Now()
	frames := make([]model.Storyboard, 0, len(drafts))
	for _, d := range drafts {
		frames = append(frames, model.Storyboard{
			ID: idgen.New(), EpisodeID: req.EpisodeID, FrameIndex: d.FrameIndex,
			Text: d.Text, VideoPrompt: d.VideoPrompt,
			Duration:  float64(d.Duration),
			RolesJSON: namesJSON(d.Roles), ScenesJSON: scenesJSON(d.Scenes), ItemsJSON: itemsJSON(d.Items),
			VideoModel: "doubao-seedance-2.0-mini",
			CreatedAt:  now, UpdatedAt: now,
		})
	}

	if err := h.repo.CreateBatch(frames); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	// 返回给前端前把 rolesJson 解析成 roles 等结构化字段：
	// 前端左侧资产卡片的选中态读的是 frame.roles，
	// 不解析就会出现「描述词里有 @老师，但人物卡片没勾上」。
	for i := range frames {
		frames[i].FillRefs()
	}

	ep.Status = "storyboard"
	ep.UpdatedAt = now
	_ = h.episodeRepo.Update(ep)

	resp := gin.H{"frames": frames, "count": len(frames), "engine": "rule"}
	if client != nil && llmErr == nil {
		resp["engine"] = "llm"
		resp["model"] = service.Describe(cfg)
	}
	if llmErr != nil {
		resp["warning"] = llmErr.Error()
	}

	// 台词忠实度校验：剧本里写死的对白有没有被漏掉、重复、或拆到两段。
	// 只报不拦 —— 自动改台词的风险比漏一句更高，交给用户判断。
	if issues := service.DialogueIssues(frames, ep.ScriptText); len(issues) > 0 {
		msg := "台词校验：" + strings.Join(issues, "；")
		if prev, ok := resp["warning"].(string); ok && prev != "" {
			resp["warning"] = prev + "；" + msg
		} else {
			resp["warning"] = msg
		}
		log.Printf("分镜台词校验发现 %d 个问题：%v", len(issues), issues)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "分镜生成成功", "data": resp})
}

// List 拉取分镜
func (h *StoryboardHandler) List(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	episodeID := c.Query("episodeId")
	if episodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}
	if _, err := h.ownedEpisode(episodeID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	frames, err := h.repo.ListByEpisodeID(episodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	// 把 rolesJson 等字符串解析成结构化列表，前端直接读 frame.roles
	for i := range frames {
		frames[i].FillRefs()
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"frames": frames}})
}

// Update 保存单镜修改
func (h *StoryboardHandler) Update(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	f, err := h.ownedFrame(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "分镜不存在"})
		return
	}

	// 全部用指针：只覆盖请求里出现的字段。
	// 否则「只改出镜人物」的请求会把文案和描述词一起清空（资产编辑踩过同样的坑）。
	var req struct {
		Text        *string           `json:"text"`
		VideoPrompt *string           `json:"videoPrompt"`
		Roles       *[]model.AssetRef `json:"roles"`
		Scenes      *[]model.AssetRef `json:"scenes"`
		Items       *[]model.AssetRef `json:"items"`
		VideoModel  *string           `json:"videoModel"`
		VideoURL    *string           `json:"videoUrl"`
		// Duration 这一帧的出片时长（秒），工具栏「批量应用」会逐帧写
		Duration *float64 `json:"duration"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	if req.Text != nil {
		f.Text = *req.Text
	}
	if req.VideoPrompt != nil {
		f.VideoPrompt = *req.VideoPrompt
	}
	if req.Roles != nil {
		if b, e := json.Marshal(*req.Roles); e == nil {
			f.RolesJSON = string(b)
		}
	}
	if req.Scenes != nil {
		if b, e := json.Marshal(*req.Scenes); e == nil {
			f.ScenesJSON = string(b)
		}
	}
	if req.Items != nil {
		if b, e := json.Marshal(*req.Items); e == nil {
			f.ItemsJSON = string(b)
		}
	}
	if req.VideoModel != nil {
		f.VideoModel = *req.VideoModel
	}
	if req.Duration != nil {
		f.Duration = *req.Duration
		// 只改时长、没同时给新描述词时，必须把描述词里的时间轴一起缩放。
		// 否则出现「卡片显示 13 秒、描述词写 0.00–3.00s」：出片时传给时长节点
		// 的是 Duration，模型会把 3 秒的动作硬拉成 13 秒，后半段基本静止 ——
		// 用户看到的是「模型不动了」，其实是提示词和时长脱节。
		if req.VideoPrompt == nil && f.VideoPrompt != "" && *req.Duration > 0 {
			f.VideoPrompt = service.RescalePromptDuration(f.VideoPrompt, int(*req.Duration))
		}
	}
	if req.VideoURL != nil {
		// 视频地址只允许空值 / 本站静态路径 / 外链，防止注入
		u := strings.TrimSpace(*req.VideoURL)
		if u != "" && !strings.HasPrefix(u, "/static/") && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "videoUrl 只能是本站静态路径或 http(s) 外链"})
			return
		}
		f.VideoURL = u
	}
	f.UpdatedAt = time.Now()

	if err := h.repo.Update(f); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	f.FillRefs()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存", "data": f})
}

// DeleteFrame 删除单个分镜（整帧：文案 / 描述词 / 视频一起删）
func (h *StoryboardHandler) DeleteFrame(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	f, err := h.ownedFrame(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "分镜不存在"})
		return
	}
	if err := h.repo.Delete(f.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// Regenerate 重新生成单镜
func (h *StoryboardHandler) Regenerate(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	f, err := h.ownedFrame(c.Param("id"), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "分镜不存在"})
		return
	}

	// Duration 工具栏选的出片时长（秒）：>0 时描述词的时间轴按它生成
	var req struct {
		Duration int `json:"duration"`
	}
	_ = c.ShouldBindJSON(&req)

	ep, _ := h.episodeRepo.GetByID(f.EpisodeID)
	if ep == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}

	chars, _ := h.charRepo.ListByEpisodeID(f.EpisodeID)
	scenes, _ := h.sceneRepo.ListByEpisodeID(f.EpisodeID)
	items, _ := h.itemRepo.ListByEpisodeID(f.EpisodeID)

	client, _, _ := h.provider.LLM()
	svc := service.NewLLMStoryboardService(client)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	// 单帧重新生成只拿 f.Text（画面脚本），里面没有台词 ——
	// 不把旧描述词里的台词带上，重新生成一次台词就全没了
	oldLines := service.ParsePromptDialogues(f.VideoPrompt)
	script := f.Text + service.DialoguesPrompt(oldLines)
	drafts, _ := svc.GenerateFrames(ctx, script, chars, scenes, items, req.Duration)
	if len(drafts) > 0 {
		drafts[0].EpisodeNo = ep.EpisodeNumber
		d := service.ApplyDialogues(drafts[0], oldLines)
		f.Text = d.Text
		f.VideoPrompt = d.VideoPrompt
		f.Duration = float64(d.Duration)
		f.UpdatedAt = time.Now()
		_ = h.repo.Update(f)
	}

	// 同样要把 rolesJson 解析出来再返回，否则前端拿到 roles=null，
	// 左侧人物/场景卡片会立刻掉选中态
	f.FillRefs()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已重新生成", "data": f})
}

func (h *StoryboardHandler) ownedEpisode(episodeID, userID string) (*model.Episode, error) {
	ep, err := h.episodeRepo.GetByID(episodeID)
	if err != nil {
		return nil, err
	}
	if !h.projectRepo.Exists(ep.ProjectID, userID) {
		return nil, errNotFound
	}
	return ep, nil
}

func (h *StoryboardHandler) ownedFrame(frameID, userID string) (*model.Storyboard, error) {
	f, err := h.repo.GetByID(frameID)
	if err != nil {
		return nil, err
	}
	ep, err := h.episodeRepo.GetByID(f.EpisodeID)
	if err != nil {
		return nil, err
	}
	if !h.projectRepo.Exists(ep.ProjectID, userID) {
		return nil, errForbidden
	}
	return f, nil
}

// ---------- JSON 序列化辅助 ----------

func namesJSON(v []model.Character) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, c := range v {
		if i > 0 {
			s += ","
		}
		s += `{"name":"` + c.Name + `","id":"` + c.ID + `"}`
	}
	return s + "]"
}

func scenesJSON(v []model.Scene) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, c := range v {
		if i > 0 {
			s += ","
		}
		s += `{"name":"` + c.Name + `","id":"` + c.ID + `"}`
	}
	return s + "]"
}

func itemsJSON(v []model.Item) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, c := range v {
		if i > 0 {
			s += ","
		}
		s += `{"name":"` + c.Name + `","id":"` + c.ID + `"}`
	}
	return s + "]"
}

var _ = errors.Is

// GenerateStream 逐段生成分镜，边生成边用 SSE 推给前端。
//
// 为什么单独一个接口：一次性生成时用户只能干等几十秒，
// 而且写到后面注意力被前面段落挤占、段与段之间衔接差。
// 逐段生成每段都带上「上一段的尾帧」，位置和姿态是传下去的。
func (h *StoryboardHandler) GenerateStream(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	var req struct {
		EpisodeID string `json:"episodeId" binding:"required"`
		Duration  int    `json:"duration"`
		// SceneRef 出片时场景用哪份素材（image/panorama/video）。
		// 在这里定下来，提示词的详略按它写，并记到分镜上供出片时默认使用。
		SceneRef string `json:"sceneRef"`
		// Outline 用户在确认弹窗里看过（可能还改过）的大纲。
		// 传了就不再重新规划 —— 不然拆法一变，用户刚确认的东西就白确认了。
		Outline []service.OutlineItem `json:"outline"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}

	ep, err := h.ownedEpisode(req.EpisodeID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}
	if strings.TrimSpace(ep.ScriptText) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "这一集还没有剧本"})
		return
	}

	chars, _ := h.charRepo.ListByEpisodeID(req.EpisodeID)
	scenes, _ := h.sceneRepo.ListByEpisodeID(req.EpisodeID)
	items, _ := h.itemRepo.ListByEpisodeID(req.EpisodeID)

	client, _, _ := h.provider.LLM()
	svc := service.NewLLMStoryboardService(client)

	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no") // 反代（nginx）不要缓冲，否则前端要等一大坨才收到
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "当前服务不支持流式输出"})
		return
	}
	send := func(ev service.StreamEvent) {
		if b, e := json.Marshal(ev); e == nil {
			fmt.Fprintf(c.Writer, "data: %s\n\n", b)
			flusher.Flush()
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 600*time.Second)
	defer cancel()

	drafts, err := svc.GenerateStream(ctx, ep.ScriptText, chars, scenes, items, req.Duration, req.SceneRef, req.Outline, send)
	if err != nil {
		send(service.StreamEvent{Type: "error", Message: err.Error()})
		return
	}
	if len(drafts) == 0 {
		send(service.StreamEvent{Type: "error", Message: "剧本太短，无法拆分镜头"})
		return
	}

	for i := range drafts {
		drafts[i].EpisodeNo = ep.EpisodeNumber
		// 描述词按场景参考类型拼：video 模式下地点行不写家具
		drafts[i].SceneRef = req.SceneRef
		drafts[i].VideoPrompt = service.BuildFrameVideoPrompt(drafts[i], drafts[i].Duration)
	}

	// 全部成功后再覆盖旧分镜：中途失败不会把用户原来的分镜清掉
	_ = h.repo.DeleteByEpisodeID(req.EpisodeID)

	now := time.Now()
	frames := make([]model.Storyboard, 0, len(drafts))
	for _, d := range drafts {
		frames = append(frames, model.Storyboard{
			ID: idgen.New(), EpisodeID: req.EpisodeID, FrameIndex: d.FrameIndex,
			Text: d.Text, VideoPrompt: d.VideoPrompt,
			SceneRef:  req.SceneRef,
			Duration:  float64(d.Duration),
			RolesJSON: namesJSON(d.Roles), ScenesJSON: scenesJSON(d.Scenes), ItemsJSON: itemsJSON(d.Items),
			VideoModel: "doubao-seedance-2.0-mini",
			CreatedAt:  now, UpdatedAt: now,
		})
	}
	if err := h.repo.CreateBatch(frames); err != nil {
		send(service.StreamEvent{Type: "error", Message: err.Error()})
		return
	}

	ep.Status = "storyboard"
	ep.UpdatedAt = now
	_ = h.episodeRepo.Update(ep)

	warning := ""
	if issues := service.DialogueIssues(frames, ep.ScriptText); len(issues) > 0 {
		warning = "台词校验：" + strings.Join(issues, "；")
		log.Printf("分镜台词校验发现 %d 个问题：%v", len(issues), issues)
	}

	send(service.StreamEvent{Type: "done", Count: len(frames), Message: warning})
}

// Outline 只生成「叙事单元大纲」，给前端弹窗确认用，不落库、不扣积分。
//
// 为什么单独一个接口：整集一次生成要几十秒，用户看不到中间过程，
// 拆错了只能整集重跑。先出大纲让他确认（或改），确认完再逐段展开。
func (h *StoryboardHandler) Outline(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	var req struct {
		EpisodeID string `json:"episodeId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
		return
	}

	ep, err := h.ownedEpisode(req.EpisodeID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "剧集不存在"})
		return
	}
	if strings.TrimSpace(ep.ScriptText) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "这一集还没有剧本"})
		return
	}

	chars, _ := h.charRepo.ListByEpisodeID(req.EpisodeID)
	scenes, _ := h.sceneRepo.ListByEpisodeID(req.EpisodeID)
	items, _ := h.itemRepo.ListByEpisodeID(req.EpisodeID)

	client, _, _ := h.provider.LLM()
	svc := service.NewLLMStoryboardService(client)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()

	outline, err := svc.PlanOutline(ctx, ep.ScriptText, chars, scenes, items)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "message": "ok",
		"data": gin.H{"outline": outline, "count": len(outline)},
	})
}
