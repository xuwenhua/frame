package database

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/config"
)

func Init(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	switch cfg.Driver {
	case "sqlite":
		db, err = gorm.Open(sqlite.Open(cfg.DSN), gormConfig)
	case "postgres":
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode)
		db, err = gorm.Open(postgres.Open(dsn), gormConfig)
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s", cfg.Driver)
	}

	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	log.Println("数据库连接成功")

	// SQLite 只允许一个写者。批量出图会同时插入/更新大量任务，
	// 多连接并发写会直接撞 SQLITE_BUSY 导致任务被静默丢弃。
	// 这里把连接池收到 1：所有读写串行，单条 SQL 都在毫秒级，对本项目完全够用。
	if cfg.Driver == "sqlite" {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.SetMaxOpenConns(1)
			sqlDB.SetMaxIdleConns(1)
			sqlDB.SetConnMaxLifetime(0)
		}
	}

	if err := autoMigrate(db); err != nil {
		return nil, fmt.Errorf("自动迁移失败: %w", err)
	}

	if err := seed(db); err != nil {
		log.Printf("初始化数据失败（可忽略）: %v", err)
	}

	return db, nil
}

func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.Project{},
		&model.Episode{},
		&model.Character{},
		&model.Scene{},
		&model.Item{},
		&model.Storyboard{},
		&model.ModelConfig{},
		&model.SystemSetting{},
		&model.AdminUser{},
		&model.User{},
		&model.PointsAccount{},
		&model.PointsTransaction{},
		&model.GenerationTask{},
		&model.FirstFrame{},
	)
}

// seed 首次启动写入默认模型与设置，已有数据时跳过
func seed(db *gorm.DB) error {
	var count int64
	db.Model(&model.ModelConfig{}).Count(&count)

	if count == 0 {
		now := time.Now()
		presets := []model.ModelConfig{
			{
				ID: "preset-llm-openai", Provider: "openai", Category: "llm",
				DisplayName: "GPT-4o", ModelID: "gpt-4o",
				BaseURL: "https://api.openai.com/v1",
				Params:  `{"temperature":0.7,"maxTokens":4096}`,
				CreditsPerCall: 14, IsDefault: true, Enabled: true, SortOrder: 1,
				Remark: "用于分镜推理与资产识别",
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-llm-deepseek", Provider: "custom", Category: "llm",
				DisplayName: "DeepSeek Chat", ModelID: "deepseek-chat",
				BaseURL: "https://api.deepseek.com/v1",
				Params:  `{"temperature":0.7,"maxTokens":8192}`,
				CreditsPerCall: 8, Enabled: true, SortOrder: 2,
				Remark: "国内可用，成本低",
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-image-rh", Provider: "runninghub", Category: "image",
				DisplayName: "RH G-2.5 Sunburst", ModelID: "rhart-image-g-2.5-sunburst",
				BaseURL: "https://www.runninghub.cn/openapi/v2",
				Params:  `{"width":1024,"height":1536,"steps":30}`,
				CreditsPerCall: 0, CreditsPerUnit: 2, IsDefault: true, Enabled: true, SortOrder: 1,
				Remark: "支持 i2i 与 t2i",
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-image-doubao", Provider: "doubao", Category: "image",
				DisplayName: "豆包 Seedream", ModelID: "doubao-seedream-3.0",
				BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
				Params:  `{"size":"1024x1536"}`,
				CreditsPerUnit: 2, Enabled: true, SortOrder: 2,
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-video-seedance-mini", Provider: "doubao", Category: "video",
				DisplayName: "Seedance 2.0 Mini", ModelID: "doubao-seedance-2.0-mini",
				BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
				Params:  `{"duration":13,"resolution":"720p","fps":24}`,
				CreditsPerCall: 35, IsDefault: true, Enabled: true, SortOrder: 1,
				Remark: "13s 720P，性价比款",
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-video-wan", Provider: "custom", Category: "video",
				DisplayName: "Wan 3.0", ModelID: "wan-3.0",
				BaseURL: "",
				Params:  `{"duration":10,"resolution":"1080p","fps":30}`,
				CreditsPerCall: 50, Enabled: true, SortOrder: 2,
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-video-seedance-pro", Provider: "doubao", Category: "video",
				DisplayName: "Seedance 2.0 Pro", ModelID: "doubao-seedance-2.0-pro",
				BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
				Params:  `{"duration":13,"resolution":"1080p","fps":24}`,
				CreditsPerCall: 80, Enabled: true, SortOrder: 3,
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-tts-minimax", Provider: "minimax", Category: "tts",
				DisplayName: "MiniMax H3", ModelID: "minimax-h3",
				BaseURL: "",
				Params:  `{"voice":"female-tianmei"}`,
				CreditsPerCall: 5, IsDefault: true, Enabled: true, SortOrder: 1,
				Remark: "出镜人物对口型配音",
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: "preset-tts-doubao", Provider: "doubao", Category: "tts",
				DisplayName: "豆包 seed-tts-2.0", ModelID: "doubao-seed-tts-2.0",
				BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
				Params:  `{"voice":"zh_female_qingxin"}`,
				CreditsPerCall: 3, Enabled: true, SortOrder: 2,
				Remark: "纯 B-roll 旁白",
				CreatedAt: now, UpdatedAt: now,
			},
		}
		if err := db.Create(&presets).Error; err != nil {
			return err
		}
		log.Printf("已写入 %d 条预设模型配置", len(presets))
	}

	// RH 中转预设（幂等：按固定 ID 判断，已存在则跳过，软删除的不复活）
	rhPresets := []model.ModelConfig{
		{
			ID: "preset-rh-proxy-image", Provider: "rh-proxy", Category: "image",
			DisplayName: "RH 中转 · Qwen 文生图", ModelID: "qwen-text2img",
			BaseURL: "http://localhost",
			Params:  `{"workflow":"qwen-text2img","aspectRatio":"9:16","queueTimeoutMs":180000}`,
			CreditsPerUnit: 2, Enabled: true, SortOrder: 5,
			Remark: "走 RunningHub 中转网关，纯文生图",
		},
		{
			ID: "preset-rh-proxy-image-krea", Provider: "rh-proxy", Category: "image",
			DisplayName: "RH 中转 · Krea2 文生图", ModelID: "krea2-text2img",
			BaseURL: "http://localhost",
			Params:  `{"workflow":"krea2-text2img","aspectRatio":"9:16","queueTimeoutMs":180000}`,
			CreditsPerUnit: 2, Enabled: true, SortOrder: 6,
			Remark: "支持中文多行场景描述",
		},
		{
			ID: "preset-rh-proxy-image-i2i", Provider: "rh-proxy", Category: "image",
			DisplayName: "RH 中转 · Qwen 图生图", ModelID: "qwen-image-2.1-pe",
			BaseURL: "http://localhost",
			Params:  `{"workflow":"qwen-image-2.1-pe","aspectRatio":"9:16","queueTimeoutMs":180000}`,
			CreditsPerUnit: 2, Enabled: true, SortOrder: 7, NeedRefImages: 2,
			Remark: "图生图 · 换装换背景，需 2 张参考图（{{ref 1}}/{{ref 2}}）",
		},
		{
			ID: "preset-rh-proxy-video", Provider: "rh-proxy", Category: "video",
			DisplayName: "RH 中转 · MiniMax H3 说话视频", ModelID: "minimax-h3-final",
			BaseURL: "http://localhost",
			Params:  `{"workflow":"minimax-h3-final","aspectRatio":"9:16","queueTimeoutMs":180000}`,
			CreditsPerCall: 35, Enabled: true, SortOrder: 4,
			Remark: "图/音/视频参考生成说话视频",
		},
	}
	for _, p := range rhPresets {
		var exist int64
		db.Unscoped().Model(&model.ModelConfig{}).Where("id = ?", p.ID).Count(&exist)
		if exist > 0 {
			continue
		}
		p.CreatedAt = time.Now()
		p.UpdatedAt = time.Now()
		if err := db.Create(&p).Error; err != nil {
			log.Printf("写入 RH 中转预设失败 %s: %v", p.ID, err)
		}
	}

	// 老库的 RH 预设还留着 900000（15 分钟）的排队上限。
	// 上游公共队列堵住时用户要干等 15 分钟才看到失败，体验极差，
	// 这里把仍然是 900000 的改到 180000（3 分钟）。
	//
	// 注意：库里存的是原始 JSON 字符串，格式不统一
	// （有的是 `"queueTimeoutMs": 900000`，有的是 `"queueTimeoutMs":900000`），
	// 所以用 Go 侧解析再回写，而不是 SQL REPLACE —— REPLACE 只能匹配一种写法。
	migrateQueueTimeout(db, "rh-proxy", 900000, 180000)

	// 积分设置
	var settingCount int64
	db.Model(&model.SystemSetting{}).Count(&settingCount)
	if settingCount == 0 {
		now := time.Now()
		settings := []model.SystemSetting{
			{Key: "points_identify", Value: "14", Group: "points", Label: "资产识别扣费", ValueType: "int", UpdatedAt: now},
			{Key: "points_image", Value: "2", Group: "points", Label: "单张图片扣费", ValueType: "int", UpdatedAt: now},
			{Key: "points_video", Value: "35", Group: "points", Label: "单条视频扣费", ValueType: "int", UpdatedAt: now},
			{Key: "points_register_bonus", Value: "100", Group: "points", Label: "注册赠送积分", ValueType: "int", UpdatedAt: now},
			{Key: "site_name", Value: "Frame", Group: "general", Label: "站点名称", ValueType: "string", UpdatedAt: now},
			{Key: "site_slogan", Value: "一句话，拍成一部短剧", Group: "general", Label: "站点标语", ValueType: "string", UpdatedAt: now},
			{Key: "max_frames_per_episode", Value: "30", Group: "general", Label: "单集最大镜头数", ValueType: "int", UpdatedAt: now},
		}
		if err := db.Create(&settings).Error; err != nil {
			return err
		}
		log.Printf("已写入 %d 条系统设置", len(settings))
	}

	// 默认管理员账号：admin@frame.local / admin123
	var adminCount int64
	db.Model(&model.User{}).Where("email = ?", "admin@frame.local").Count(&adminCount)
	if adminCount == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		if err == nil {
			db.Create(&model.User{
				ID:           "admin-default-account",
				Email:        "admin@frame.local",
				PasswordHash: string(hash),
				Nickname:     "管理员",
				Status:       "active",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			})
			log.Println("已创建默认管理员：admin@frame.local / admin123")
		}
	}

	return nil
}

// migrateQueueTimeout 把指定 provider 下排队超时等于 oldVal 的模型改成 newVal
//
// 为什么要解析再回写：params 是原始 JSON 字符串，历史数据里空格写法不统一
// （`"queueTimeoutMs": 900000` 与 `"queueTimeoutMs":900000` 都有），
// 用 SQL REPLACE 只能命中其中一种。这里用 map 解析，两种写法都能处理，
// 且只改「正好等于旧默认值」的，管理员手动调过的保持不动。
func migrateQueueTimeout(db *gorm.DB, provider string, oldVal, newVal int) {
	var list []model.ModelConfig
	if err := db.Where("provider = ?", provider).Find(&list).Error; err != nil {
		log.Printf("迁移排队超时：查询失败 %v", err)
		return
	}
	changed := 0
	for _, m := range list {
		if strings.TrimSpace(m.Params) == "" {
			continue
		}
		var p map[string]interface{}
		if err := json.Unmarshal([]byte(m.Params), &p); err != nil {
			continue
		}
		v, ok := p["queueTimeoutMs"]
		if !ok {
			continue
		}
		// JSON 数字解析成 float64
		if f, isNum := v.(float64); !isNum || int(f) != oldVal {
			continue
		}
		p["queueTimeoutMs"] = newVal
		raw, err := json.Marshal(p)
		if err != nil {
			continue
		}
		if err := db.Model(&model.ModelConfig{}).Where("id = ?", m.ID).
			Update("params", string(raw)).Error; err != nil {
			log.Printf("迁移排队超时 %s 失败: %v", m.ID, err)
			continue
		}
		changed++
	}
	if changed > 0 {
		log.Printf("已把 %d 个模型的排队超时从 %dms 调整为 %dms", changed, oldVal, newVal)
	}
}
