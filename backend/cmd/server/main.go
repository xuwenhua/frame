package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"jikeai-clone/internal/handler"
	"jikeai-clone/internal/middleware"
	"jikeai-clone/internal/model"
	"jikeai-clone/internal/queue"
	"jikeai-clone/internal/repository"
	"jikeai-clone/internal/service"
	"jikeai-clone/pkg/auth"
	"jikeai-clone/pkg/config"
	"jikeai-clone/pkg/database"
)

func main() {
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	db, err := database.Init(&cfg.Database)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	// 补齐内置模型清单：配置只存在本机 SQLite，换机器就空了。
	// 按 model_id 比对，缺的补、已有不动（用户的 key 和参数优先）。
	service.SeedModelConfigs(db)

	// ===== Repository =====
	projectRepo := repository.NewProjectRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	charRepo := repository.NewCharacterRepository(db)
	sceneRepo := repository.NewSceneRepository(db)
	itemRepo := repository.NewItemRepository(db)
	storyboardRepo := repository.NewStoryboardRepository(db)
	firstFrameRepo := repository.NewFirstFrameRepository(db)
	modelConfigRepo := repository.NewModelConfigRepository(db)
	settingRepo := repository.NewSettingRepository(db)
	userRepo := repository.NewUserRepository(db)
	pointsRepo := repository.NewPointsRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	statsRepo := repository.NewStatsRepository(db)

	// ===== 基础设施 =====
	jwtMgr := auth.NewManager(cfg.JWT.Secret, cfg.JWT.Expire)
	provider := service.NewModelProvider(modelConfigRepo)

	// 一次性清洗：历史角色描述里可能残留「坐姿微微前倾」这类瞬时姿态，
	// 会让资产图出成坐姿。幂等，已干净的行不动。
	service.CleanPoseInCharacterPrompts(db)

	// 任务执行器：一个任务一个协程，有多少任务就同时提交多少给上游
	// 产物转存：RunningHub 的结果 URL 24 小时失效，成功即下载到本地
	resultStore := service.NewResultStore(cfg.App.ResultDir, cfg.App.ResultURLPrefix)
	q := queue.New(db, pointsRepo, cfg.App.MaxConcurrentTasks).WithResultStore(resultStore)
	registerQueueHandlers(q, db, modelConfigRepo, provider, resultStore, cfg.App.SketchModelID)
	q.Start()
	q.Recover()

	// ===== Handler =====
	userHandler := handler.NewUserHandler(userRepo, pointsRepo, settingRepo, jwtMgr)
	projectHandler := handler.NewProjectHandler(projectRepo)
	episodeHandler := handler.NewEpisodeHandler(episodeRepo, projectRepo)
	identifyHandler := handler.NewIdentifyHandler(
		charRepo, sceneRepo, itemRepo, episodeRepo, projectRepo, pointsRepo, settingRepo, provider)
	storyboardHandler := handler.NewStoryboardHandler(
		storyboardRepo, episodeRepo, projectRepo, charRepo, sceneRepo, itemRepo, pointsRepo, settingRepo, provider)
	generateHandler := handler.NewGenerateHandler(
		taskRepo, projectRepo, episodeRepo, charRepo, sceneRepo, itemRepo,
		storyboardRepo, firstFrameRepo, pointsRepo, settingRepo, q, resultStore.Dir())
	firstFrameHandler := handler.NewFirstFrameHandler(
		provider, firstFrameRepo, storyboardRepo, episodeRepo, projectRepo,
		charRepo, sceneRepo, itemRepo, taskRepo, pointsRepo, settingRepo, q,
		resultStore.Dir(), cfg.App.FFmpegPath)
	adminHandler := handler.NewAdminHandler(modelConfigRepo, settingRepo, provider)
	adminUserHandler := handler.NewAdminUserHandler(userRepo, pointsRepo, projectRepo)
	uploadHandler := handler.NewUploadHandler(filepath.Join(resultStore.Dir(), "uploads"), "/static/uploads")
	adminTaskHandler := handler.NewAdminTaskHandler(taskRepo, q)
	accountHandler := handler.NewAccountHandler(userRepo, pointsRepo, statsRepo)
	// 成片合成：把一集里已出片的分镜按顺序拼成一条
	composer := service.NewVideoComposer(cfg.App.FFmpegPath, resultStore.Dir(), resultStore.Prefix())
	composeHandler := handler.NewComposeHandler(
		episodeRepo, projectRepo, storyboardRepo, composer, resultStore.Dir())

	// ===== 路由 =====
	gin.SetMode(cfg.App.Mode)
	r := gin.Default()
	r.Use(middleware.CORS())
	r.Use(middleware.Logger())

	// 生成产物静态目录（转存后的图片 / 视频）
	r.Static(resultStore.Prefix(), resultStore.Dir())
	// 用户上传目录（角色参考图 / 垫图 / 参考音频），独立前缀便于区分与清理
	r.Static("/static/uploads", filepath.Join(resultStore.Dir(), "uploads"))

	api := r.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok", "app": cfg.App.Name, "version": cfg.App.Version})
		})

		// ---------- 公开接口 ----------
		public := api.Group("/auth")
		{
			public.POST("/register", userHandler.Register)
			public.POST("/login", userHandler.Login)
		}

		// ---------- 需要登录 ----------
		authed := api.Group("")
		authed.Use(middleware.Auth(jwtMgr), middleware.RequireActiveUser(userRepo))
		{
			// 可用模型清单（给创作端选模型用，不含密钥）
			authed.GET("/models", func(c *gin.Context) {
				list, err := modelConfigRepo.List(c.Query("category"))
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
					return
				}
				out := make([]gin.H, 0, len(list))
				for _, m := range list {
					if !m.Enabled {
						continue
					}
					workflow := ""
					needRef := 0
					duration := 0
					aspect := ""
					if m.Params != "" {
						var p struct {
							Workflow      string `json:"workflow"`
							NeedRefImages *int   `json:"needRefImages"`
							Duration      int    `json:"duration"`
							AspectRatio   string `json:"aspectRatio"`
						}
						if json.Unmarshal([]byte(m.Params), &p) == nil {
							workflow = p.Workflow
							duration = p.Duration
							aspect = p.AspectRatio
							if p.NeedRefImages != nil {
								needRef = *p.NeedRefImages
							} else if spec := service.RHWorkflowByName(p.Workflow); spec != nil {
								// 没显式配置时按工作流本身的必填图位推断
								needRef = spec.RequiredImg
							}
							// 模型没写时长就取工作流默认值，前端据此显示「真实可用」的时长
							if duration <= 0 {
								if spec := service.RHWorkflowByName(p.Workflow); spec != nil {
									duration = spec.DefaultDuration
								}
							}
							// 画幅同理：模型没写就取工作流默认。
							// 不兜底的话前端会显示「无画幅」，但实际出片有固定比例
							// （如 minimax-h3-i2v-7img 的 115 节点默认 9:16），
							// 用户看不到真实情况，容易以为是横屏。
							if aspect == "" {
								if spec := service.RHWorkflowByName(p.Workflow); spec != nil {
									aspect = spec.DefaultAspect
								}
							}
						}
					}
					out = append(out, gin.H{
						"id": m.ID, "displayName": m.DisplayName, "category": m.Category,
						"provider": m.Provider, "workflow": workflow,
						"needRefImages": needRef,
						"duration":      duration,
						"aspectRatio":   aspect,
						"isDefault":     m.IsDefault, "hasKey": m.APIKey != "",
					})
				}
				c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": out})
			})

			// 用户
			me := authed.Group("/me")
			{
				me.GET("", userHandler.Profile)
				me.GET("/points", userHandler.PointsBalance)
				me.GET("/points/transactions", userHandler.PointsTransactions)

				// 个人中心
				me.GET("/overview", accountHandler.Overview)
				me.GET("/stats", accountHandler.Stats)
				me.PUT("", accountHandler.UpdateProfile)
				me.PUT("/password", accountHandler.ChangePassword)
			}

			// 资产（编辑：名称 / 提示词 / 参考图 / 音色 / 参考音频；删除）
			authed.PUT("/assets/:type/:id", identifyHandler.UpdateAsset)
			authed.DELETE("/assets/:type/:id", identifyHandler.DeleteAsset)

			// 用户上传（角色参考图、图生图垫图、角色参考音频）
			authed.POST("/upload", uploadHandler.Upload)

			// 项目
			projects := authed.Group("/projects")
			{
				projects.GET("", projectHandler.List)
				projects.POST("", projectHandler.Create)
				projects.GET("/:id", projectHandler.Get)
				projects.PUT("/:id", projectHandler.Update)
				projects.DELETE("/:id", projectHandler.Delete)
				// 项目资产库（跨剧集聚合 / 新建 / 从总库导入）
				projects.GET("/:id/assets", identifyHandler.ProjectAssets)
				projects.POST("/:id/assets/:type", identifyHandler.CreateProjectAsset)
				projects.POST("/:id/assets/:type/import", identifyHandler.ImportProjectAsset)
			}

			// 总角色场景库（该用户所有项目的资产池）
			authed.GET("/library/assets", identifyHandler.GlobalLibrary)

			// 剧集
			episodes := authed.Group("/episodes")
			{
				episodes.GET("", episodeHandler.List)
				episodes.POST("", episodeHandler.Create)
				episodes.GET("/:id", episodeHandler.Get)
				episodes.PUT("/:id/script", episodeHandler.UpdateScript)
				episodes.PUT("/:id/config", episodeHandler.UpdateConfig)
				// 整集合成：把已出片的分镜按 frame_index 顺序拼成一条完整视频
				episodes.POST("/:id/compose", composeHandler.Compose)
			}

			// 识别
			identify := authed.Group("/identify")
			{
				identify.GET("", identifyHandler.List)
				identify.POST("", identifyHandler.All)
			}

			// 分镜
			storyboard := authed.Group("/storyboard")
			{
				storyboard.GET("", storyboardHandler.List)
				storyboard.POST("/generate", storyboardHandler.Generate)
				// 逐段生成 + SSE：先出大纲，再一段段推给前端
				storyboard.POST("/generate/stream", storyboardHandler.GenerateStream)
				// 只出大纲，给前端弹窗确认；确认后再调 generate/stream 展开
				storyboard.POST("/outline", storyboardHandler.Outline)
				storyboard.PUT("/:id", storyboardHandler.Update)
				storyboard.DELETE("/:id", storyboardHandler.DeleteFrame)
				storyboard.POST("/:id/regenerate", storyboardHandler.Regenerate)
				// 整集宫格草图（简笔速写，只作构图参考）
				// 首帧图：角色图 + 场景图 + 构图描述 → 真实画面，进视频首帧位
				storyboard.POST("/firstframe", generateHandler.GenerateFirstFrame)
				// 首帧规划：按「场景 + 机位」归并，一集只出几张（同机位多段共用）
				episodes.POST("/:id/firstframe-plan", firstFrameHandler.Plan)
				episodes.GET("/:id/firstframes", firstFrameHandler.List)
				episodes.POST("/:id/firstframes/generate", firstFrameHandler.Generate)
				r.PUT("/api/firstframes/:id", firstFrameHandler.UpdateGroup)
				// 整张宫格图列表：裁成单格后「一眼看整段节奏」的信息就没了，
				// 这里把每段原图单独列出来给前端做整段预览
				storyboard.GET("/grids", func(c *gin.Context) {
					epID := c.Query("episodeId")
					if epID == "" {
						c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 episodeId"})
						return
					}
					files, _ := filepath.Glob(filepath.Join(resultStore.Dir(),
						"sketch-grid-"+epID+"-*.png"))
					sort.Strings(files) // 文件名里段号补了零，字典序就是段序
					urls := make([]string, 0, len(files))
					for _, f := range files {
						urls = append(urls, resultStore.Prefix()+"/"+filepath.Base(f))
					}
					c.JSON(http.StatusOK, gin.H{"code": 0, "data": urls})
				})
			}

			// 生成
			gen := authed.Group("/generate")
			{
				gen.POST("/image", generateHandler.GenerateImage)
				gen.POST("/images", generateHandler.BatchGenerateImages)
				gen.POST("/video", generateHandler.GenerateVideo)
				// 场景全景：先转柱形全景图，再转 360 视频
				gen.POST("/panorama", generateHandler.GeneratePanorama)
				gen.POST("/panorama-video", generateHandler.GeneratePanoramaVideo)
			}

			// 任务
			tasks := authed.Group("/tasks")
			{
				tasks.GET("", generateHandler.ListTasks)
				tasks.GET("/frame/:id/history", generateHandler.FrameTaskHistory)
				tasks.GET("/:id", generateHandler.TaskStatus)
			}
		}

		// ---------- 管理端（需管理员 token） ----------
		admin := api.Group("/admin")
		admin.Use(middleware.Auth(jwtMgr), middleware.RequireAdmin())
		{
			admin.GET("/overview", adminHandler.Overview)

			models := admin.Group("/models")
			{
				models.GET("", adminHandler.ListModels)
				models.POST("", adminHandler.CreateModel)
				models.PUT("/:id", adminHandler.UpdateModel)
				models.DELETE("/:id", adminHandler.DeleteModel)
				models.POST("/:id/default", adminHandler.SetDefault)
				models.POST("/:id/test", adminHandler.TestModel)
			}

			settings := admin.Group("/settings")
			{
				settings.GET("", adminHandler.ListSettings)
				settings.PUT("", adminHandler.SaveSettings)
			}

			// 用户管理
			users := admin.Group("/users")
			{
				users.GET("", adminUserHandler.List)
				users.GET("/:id", adminUserHandler.Detail)
				users.POST("/:id/points", adminUserHandler.AdjustPoints)
				users.PUT("/:id/status", adminUserHandler.SetStatus)
			}

			// 任务生成管理（跨用户实时监控）
			tasks := admin.Group("/tasks")
			{
				tasks.GET("", adminTaskHandler.List)
				tasks.GET("/stats", adminTaskHandler.Stats)
				tasks.GET("/:id", adminTaskHandler.Detail)
				tasks.POST("/:id/retry", adminTaskHandler.Retry)
			}

			// RH 中转工作流清单（离线可用，便于前端下拉选择）
			admin.GET("/rh/workflows", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": service.RHWorkflowBriefs()})
			})
		}
	}

	// ===== 优雅关闭 =====
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.App.Port),
		Handler: r,
	}

	go func() {
		log.Printf("服务器启动于 http://localhost:%d", cfg.App.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭服务…")

	q.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("服务关闭异常: %v", err)
	}

	// 关闭数据库连接，避免 SQLite 残留 journal
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	log.Println("服务已退出")
}

// fetchImageBytes 取图片字节：本站路径直接读盘，远程地址走 HTTP。
// 生图返回的可能是网关的公网 URL，也可能是已转存的 /static/results 路径。
func fetchImageBytes(ctx context.Context, url, localRoot string) ([]byte, error) {
	if rel, ok := strings.CutPrefix(url, "/static/results/"); ok {
		return os.ReadFile(filepath.Join(localRoot, rel))
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	return nil, fmt.Errorf("不认识的图片地址: %s", url)
}

// registerQueueHandlers 注册任务处理器
// sketchModelID：分镜草图专用图像模型（为空则回退图像分类默认模型）
func registerQueueHandlers(q *queue.Queue, db *gorm.DB, modelRepo *repository.ModelConfigRepository, provider *service.ModelProvider, resultStore *service.ResultStore, sketchModelID string) {
	// 图像任务：任务指定了 modelId 就用指定模型（如图生图），否则用该分类默认
	q.Register("image", func(ctx context.Context, task *model.GenerationTask) (string, string, error) {
		var (
			cfg *model.ModelConfig
			err error
		)
		if task.ModelID != "" {
			cfg, err = modelRepo.GetByID(task.ModelID)
		} else {
			cfg, err = modelRepo.GetDefault("image")
		}
		if err != nil || cfg == nil {
			return "", "", errors.New("未配置图像模型，请到管理端添加")
		}

		var refs []string
		aspect := ""
		if task.Params != "" {
			var p struct {
				RefImages []string `json:"refImages"`
				Aspect    string   `json:"aspect"`
			}
			if json.Unmarshal([]byte(task.Params), &p) == nil {
				refs = p.RefImages
				aspect = p.Aspect
			}
		}

		// 任务级画幅覆盖模型默认值（角色三视图 / 场景宽幅 / 道具方图）
		if aspect != "" {
			merged := map[string]interface{}{}
			if cfg.Params != "" {
				_ = json.Unmarshal([]byte(cfg.Params), &merged)
			}
			merged["aspectRatio"] = aspect
			if raw, e := json.Marshal(merged); e == nil {
				cp := *cfg
				cp.Params = string(raw)
				cfg = &cp
			}
		}

		gen := service.NewImageGenerator(cfg)
		// 拿到上游 taskId 立刻落库：进程重启后能凭它续查结果，不必重新提交
		gen.OnExternalTaskID = func(extID string) {
			if err := db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).
				Update("external_task_id", extID).Error; err != nil {
				log.Printf("写入上游任务 ID 失败 %s: %v", task.ID, err)
			}
		}
		// 重启恢复的任务若已有上游 ID，直接续查，不重复提交（避免白烧额度）
		gen.ExistingTaskID = task.ExternalTaskID
		// 参考图是本机文件（/static/...），网关读不到，需要先上传换 download_url
		gen.LocalRoot = resultStore.Dir()
		return gen.GenerateWithRefs(ctx, task.Prompt, refs)
	})

	// 分镜草图任务：一张图画 N 格（竖版 4×2 / 横版 2×4），成功后按格裁开写回对应帧。
	//
	// 草图不传参考图 —— 传了人物图，模型就会照着画人脸，
	// 而草图一旦有脸，生成视频时人物就会被草图带偏，参考图就白传了。
	q.Register("firstframe", func(ctx context.Context, task *model.GenerationTask) (string, string, error) {
		var (
			cfg *model.ModelConfig
			err error
		)
		if task.ModelID != "" {
			cfg, err = modelRepo.GetByID(task.ModelID)
		}
		if err != nil || cfg == nil || cfg.NeedRefImages <= 0 {
			// 没指定、或指定的不支持参考图：自己找一个图生图模型
			list, lerr := modelRepo.List("image")
			cfg = nil
			if lerr == nil {
				// 优先挑「至少 2 张参考图」的：首帧图要同时吃角色图和场景图，
				// 只支持 1 张的模型只能锁人物、锁不住空间
				for need := 2; need >= 1 && cfg == nil; need-- {
					for i := range list {
						c := &list[i]
						if c.Enabled && c.NeedRefImages >= need && strings.TrimSpace(c.APIKey) != "" {
							cfg = c
							break
						}
					}
				}
			}
			if cfg == nil {
				return "", "", errors.New("首帧图需要图生图模型（支持参考图），请到管理端配置")
			}
		}

		var refs []string
		aspect := ""
		if task.Params != "" {
			var p struct {
				RefImages []string `json:"refImages"`
				Aspect    string   `json:"aspect"`
			}
			if json.Unmarshal([]byte(task.Params), &p) == nil {
				refs = p.RefImages
				aspect = p.Aspect
			}
		}
		if len(refs) < 2 {
			return "", "", errors.New("首帧图至少需要 2 张参考图（角色 + 场景）")
		}

		if aspect != "" {
			merged := map[string]interface{}{}
			if cfg.Params != "" {
				_ = json.Unmarshal([]byte(cfg.Params), &merged)
			}
			merged["aspectRatio"] = aspect
			if raw, e := json.Marshal(merged); e == nil {
				cp := *cfg
				cp.Params = string(raw)
				cfg = &cp
			}
		}

		gen := service.NewImageGenerator(cfg)
		gen.OnExternalTaskID = func(extID string) {
			if err := db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).
				Update("external_task_id", extID).Error; err != nil {
				log.Printf("写入首帧图上游任务 ID 失败 %s: %v", task.ID, err)
			}
		}
		gen.ExistingTaskID = task.ExternalTaskID
		gen.LocalRoot = resultStore.Dir()

		url, _, gerr := gen.GenerateWithRefs(ctx, task.Prompt, refs)
		if gerr != nil {
			return "", "", gerr
		}
		// 回写：分组图写 first_frames.image（同机位多段共用），
		// 单帧图写 storyboards.first_frame_image（没做规划时的兜底）
		if task.AssetType == "group" {
			if e := db.Model(&model.FirstFrame{}).Where("id = ?", task.AssetID).
				Update("image", url).Error; e != nil {
				log.Printf("回写分组首帧图失败 %s: %v", task.AssetID, e)
			}
			log.Printf("分组首帧图完成：%s → %s", task.AssetID, url)
		} else {
			if e := db.Model(&model.Storyboard{}).Where("id = ?", task.StoryboardID).
				Update("first_frame_image", url).Error; e != nil {
				log.Printf("回写首帧图失败 %s: %v", task.StoryboardID, e)
			}
			log.Printf("首帧图完成：帧 %s → %s", task.StoryboardID, url)
		}
		return url, "", nil
	})

	q.Register("video", func(ctx context.Context, task *model.GenerationTask) (string, string, error) {
		var (
			cfg *model.ModelConfig
			err error
		)
		if task.ModelID != "" {
			cfg, err = modelRepo.GetByID(task.ModelID)
		} else {
			cfg, err = modelRepo.GetDefault("video")
		}
		if err != nil || cfg == nil {
			return "", "", errors.New("未配置视频模型，请到管理端添加")
		}

		// 分镜工具栏选的时长 / 画面比例覆盖模型默认值
		cfg = service.ApplyVideoOverrides(cfg, task.Params)

		// 参考图：以前恒传空串，9 个图位全 null，上游必报 803 JsonNull；
		// 后来只传第一张，@ 到的其余资产没有视觉参照。现在整份传下去。
		var refImages []string
		vAspect := ""
		vDuration := 0
		vVideo := ""
		var vAudios []string
		if task.Params != "" {
			var p struct {
				RefImages   []string `json:"refImages"`
				AspectRatio string   `json:"aspectRatio"`
				Duration    int      `json:"duration"`
				VideoURL    string   `json:"videoUrl"`
				AudioURLs   []string `json:"audioUrls"`
			}
			if json.Unmarshal([]byte(task.Params), &p) == nil {
				// 整份参考图都要传下去：提示词里 @ 了几个资产，上游就该收到几张图。
				// 以前只取 RefImages[0]，其余角色/场景/道具图全部丢弃，
				// 模型只能靠文字现编形象，一致性直接崩。
				refImages = p.RefImages
				vAspect = p.AspectRatio
				vDuration = p.Duration
				vVideo = p.VideoURL
				vAudios = p.AudioURLs
			}
		}

		// 提示词与工具栏对齐：去掉只给编辑器看的 @资产索引块，
		// 并把「视觉风格」的画幅、「单镜时长」改成实际提交的值。
		// 不清洗会出现「提示词写竖屏 9:16，实际出 16:9 横屏」这种自相矛盾。
		prompt := service.SanitizeVideoPrompt(task.Prompt, vAspect, vDuration)

		gen := service.NewVideoGenerator(cfg)
		gen.LocalRoot = resultStore.Dir()
		gen.OnExternalTaskID = func(extID string) {
			if err := db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).
				Update("external_task_id", extID).Error; err != nil {
				log.Printf("写入上游任务 ID 失败 %s: %v", task.ID, err)
			}
		}
		return gen.Generate(ctx, prompt, refImages, vVideo, vAudios)
	})

	// 场景 → 柱形全景图（equirectangular 2:1）。
	// 用图片编辑模型把单视角场景图补成 360 度全景，左右两端要能无缝接上 —— 后面转 360 视频全靠它。
	q.Register("panorama", func(ctx context.Context, task *model.GenerationTask) (string, string, error) {
		cfg, err := modelRepo.GetByID(task.ModelID)
		if err != nil || cfg == nil {
			return "", "", errors.New("未配置图片编辑模型，请到管理端添加")
		}
		var refs []string
		aspect := ""
		if task.Params != "" {
			var p struct {
				RefImages []string `json:"refImages"`
				Aspect    string   `json:"aspect"`
			}
			if json.Unmarshal([]byte(task.Params), &p) == nil {
				refs = p.RefImages
				aspect = p.Aspect
			}
		}
		if len(refs) == 0 {
			return "", "", errors.New("缺少场景图，无法扩展成全景")
		}
		// 画幅必须覆盖模型默认值：qwen-image 默认 9:16，直接用会出竖图，
		// 竖图转 360 时整个房间被拉成一条。
		if aspect != "" {
			merged := map[string]interface{}{}
			if cfg.Params != "" {
				_ = json.Unmarshal([]byte(cfg.Params), &merged)
			}
			merged["aspectRatio"] = aspect
			if raw, e := json.Marshal(merged); e == nil {
				cp := *cfg
				cp.Params = string(raw)
				cfg = &cp
			}
		}
		gen := service.NewImageGenerator(cfg)
		gen.LocalRoot = resultStore.Dir()
		gen.OnExternalTaskID = func(extID string) {
			if err := db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).
				Update("external_task_id", extID).Error; err != nil {
				log.Printf("写入上游任务 ID 失败 %s: %v", task.ID, err)
			}
		}
		gen.ExistingTaskID = task.ExternalTaskID
		return gen.GenerateWithRefs(ctx, task.Prompt, refs)
	})

	// 柱形全景图 → 360 全景视频（panorama-to-360video 工作流）。
	// 这个工作流只有图位 2、没有提示词/时长/画幅节点，所以 prompt 传空、只给图。
	q.Register("panorama_video", func(ctx context.Context, task *model.GenerationTask) (string, string, error) {
		cfg, err := modelRepo.GetByID(task.ModelID)
		if err != nil || cfg == nil {
			return "", "", errors.New("未配置全景视频工作流，请到管理端添加")
		}
		var refs []string
		if task.Params != "" {
			var p struct {
				RefImages []string `json:"refImages"`
			}
			if json.Unmarshal([]byte(task.Params), &p) == nil {
				refs = p.RefImages
			}
		}
		if len(refs) == 0 {
			return "", "", errors.New("缺少柱形全景图，请先生成全景图")
		}
		gen := service.NewVideoGenerator(cfg)
		gen.LocalRoot = resultStore.Dir()
		gen.OnExternalTaskID = func(extID string) {
			if err := db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).
				Update("external_task_id", extID).Error; err != nil {
				log.Printf("写入上游任务 ID 失败 %s: %v", task.ID, err)
			}
		}
		return gen.Generate(ctx, "", refs, "", nil)
	})
}
