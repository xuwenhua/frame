package service

import (
	"log"

	"gorm.io/gorm"

	"jikeai-clone/internal/model"
	"jikeai-clone/pkg/idgen"
)

// SeedModelConfigs 补齐内置模型清单。
//
// 为什么需要它：模型配置一直只存在本机的 SQLite 里（~/.jikeai-clone-data），
// 不在仓库里 —— 换一台机器、或者别人拉代码跑起来，管理端是空的，
// 得手工一条条加回来。这里把「结构」写成代码，启动时按 model_id 比对，
// 缺的补上，已有的**一个字都不改**（用户填的 key、改的参数都保留）。
//
// 注意：api_key 不写死。密钥属于部署环境，由管理员在管理端填，
// 或者走 config.yaml 里的默认值。
func SeedModelConfigs(db *gorm.DB) {
	if db == nil {
		return
	}

	type seed struct {
		Category       string
		DisplayName    string
		ModelID        string
		Provider       string
		BaseURL        string
		Params         string
		IsDefault      int
		SortOrder      int
		Remark         string
		CreditsPerCall int
	}

	seeds := []seed{
		// ---- 图像 ----
		{"image", "doubao-seedream-5.0-pro", "doubao-seedream-5.0-pro", "volcengine",
			"https://ark.cn-beijing.volces.com/api/v3", `{"size":"2K"}`, 1, 10, "出图主力", 20},
		{"image", "RH G-2.5 Sunburst", "rhart-image-g-2.5-sunburst", "rh-proxy",
			"https://www.runninghub.cn", `{"workflow":"rhart-image-g-2.5"}`, 0, 20, "RunningHub 出图", 20},
		{"image", "豆包 Seedream", "doubao-seedream-3.0", "volcengine",
			"https://ark.cn-beijing.volces.com/api/v3", `{}`, 0, 30, "", 20},
		{"image", "RH 中转 · Qwen 文生图", "qwen-text2img", "rh-proxy",
			"https://www.runninghub.cn", `{"workflow":"qwen-text2img"}`, 0, 40, "", 20},
		{"image", "RH 中转 · Krea2 文生图", "krea2-text2img", "rh-proxy",
			"https://www.runninghub.cn", `{"workflow":"krea2-text2img"}`, 0, 50, "", 20},
		{"image", "RH 中转 · Qwen 图生图", "qwen-image-2.1-pe", "rh-proxy",
			"https://www.runninghub.cn", `{"workflow":"qwen-image-2.1-pe"}`, 0, 60, "", 20},

		// ---- 文本 ----
		{"llm", "DeepSeek Chat", "auto/deepseek-v4.1-flash", "deepseek",
			"http://127.0.0.1:3425/v1", `{"temperature":0.7,"maxTokens":8192}`, 1, 10, "分镜/大纲主力，支持看图", 0},
		{"llm", "GPT-4o", "gpt-4o", "openai",
			"https://api.openai.com/v1", `{"temperature":0.7,"maxTokens":4096}`, 0, 20, "", 0},

		// ---- 语音 ----
		{"tts", "MiniMax H3", "minimax-h3", "minimax",
			"", `{}`, 1, 10, "", 0},
		{"tts", "豆包 seed-tts-2.0", "doubao-seed-tts-2.0", "volcengine",
			"https://ark.cn-beijing.volces.com/api/v3", `{"voice":"zh_female_gujie_uranus_bigtts"}`, 0, 20, "", 0},

		// ---- 视频 ----
		{"video", "RH 中转 · MiniMax H3 多参", "minimax-h3-multi", "rh-proxy",
			"https://www.runninghub.cn",
			`{"workflow":"minimax-h3-multi","queueTimeoutMs":0,"aspectRatio":"9:16","duration":10}`,
			0, 10, "9 图 + 3 音频 + 3 参考视频", 35},
		{"video", "RH 中转 · MiniMax H3 高清", "minimax-h3-i2v-7img", "rh-proxy",
			"https://www.runninghub.cn",
			`{"workflow":"minimax-h3-i2v-7img","queueTimeoutMs":0,"aspectRatio":"9:16","duration":10}`,
			0, 20, "7 图 + 1 参考视频", 35},
		{"video", "RH 中转 · MiniMax H3", "minimax-h3-final", "rh-proxy",
			"https://www.runninghub.cn",
			`{"workflow":"minimax-h3-final","queueTimeoutMs":0}`, 0, 30, "", 35},
		{"video", "RH 中转 · 360 全景视频", "panorama-to-360video", "rh-proxy",
			"https://www.runninghub.cn", `{"workflow":"panorama-to-360video"}`, 0, 40, "全景图转 360 视频", 20},
		{"video", "Seedance 2.0 Mini", "doubao-seedance-2.0-mini", "volcengine",
			"https://ark.cn-beijing.volces.com/api/v3", `{}`, 1, 50, "", 35},
		{"video", "Seedance 2.0 Pro", "doubao-seedance-2.0-pro", "volcengine",
			"https://ark.cn-beijing.volces.com/api/v3", `{}`, 0, 60, "", 60},
		{"video", "Wan 3.0", "wan-3.0", "volcengine",
			"https://ark.cn-beijing.volces.com/api/v3", `{}`, 0, 70, "", 35},
	}

	added := 0
	for _, sd := range seeds {
		// Unscoped 必须加：用户主动删掉的配置是软删除（deleted_at 有值），
		// 默认查询过滤掉它们，会导致「删了又被种子补回来」。
		// 判据是「历史上存在过这个 model_id」就跳过。
		var count int64
		db.Unscoped().Model(&model.ModelConfig{}).Where("model_id = ?", sd.ModelID).Count(&count)
		if count > 0 {
			continue
		}
		cfg := model.ModelConfig{
			ID:             idgen.New(),
			Provider:       sd.Provider,
			Category:       sd.Category,
			DisplayName:    sd.DisplayName,
			ModelID:        sd.ModelID,
			BaseURL:        sd.BaseURL,
			APIKey:         "", // 密钥不写死，管理员在管理端填
			Params:         sd.Params,
			NeedRefImages:  boolToInt(sd.Category == "image" || sd.Category == "video"),
			CreditsPerCall: sd.CreditsPerCall,
			IsDefault:      sd.IsDefault == 1,
			Enabled:        true,
			SortOrder:      sd.SortOrder,
			Remark:         sd.Remark,
		}
		if err := db.Create(&cfg).Error; err != nil {
			log.Printf("补齐模型配置失败（%s）：%v", sd.DisplayName, err)
			continue
		}
		added++
	}
	if added > 0 {
		log.Printf("已补齐 %d 条内置模型配置（密钥请到管理端填写）", added)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
