package service

import (
	"log"
	"strings"

	"gorm.io/gorm"

	"jikeai-clone/internal/model"
)

// CleanPoseInCharacterPrompts 清理历史数据里角色描述中的瞬时姿态
//
// 背景：早期识别提示词没禁止姿态描写，LLM 会写出「坐姿微微前倾」「站着时…」，
// 出图就照着画成坐姿 —— 而资产图要拿去对位所有镜头，坐姿没法用。
//
// 现在提交时（ApplyGenOptions）也会剥一次，但库里留着脏文本会让用户在
// 编辑弹窗里看到「坐着时上身微微前倾」，容易困惑。这里做一次性清洗：
// 幂等，已干净的行不会被改动，重复执行无副作用。
func CleanPoseInCharacterPrompts(db *gorm.DB) {
	var list []model.Character
	if err := db.Where("image_prompt <> ''").Find(&list).Error; err != nil {
		log.Printf("清理角色姿态描写：查询失败 %v", err)
		return
	}

	cleaned := 0
	for _, c := range list {
		next := stripPoseClauses(c.ImagePrompt)
		if strings.TrimSpace(next) == "" || next == c.ImagePrompt {
			continue
		}
		if err := db.Model(&model.Character{}).Where("id = ?", c.ID).
			Update("image_prompt", next).Error; err != nil {
			log.Printf("清理角色姿态描写 %s 失败: %v", c.ID, err)
			continue
		}
		cleaned++
	}
	if cleaned > 0 {
		log.Printf("已清理 %d 个角色描述中的姿态描写", cleaned)
	}
}
