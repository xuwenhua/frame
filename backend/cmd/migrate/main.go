package main

// 把 SQLite 里的数据整体搬到 PostgreSQL。
//
// 为什么单独写一个：项目原本用 SQLite，切 PG 后表是空的。
// GORM 的 AutoMigrate 只建表不搬数据，而数据里既有用户、项目，
// 也有分镜和任务记录，手工导出导入容易漏表或顺序错（外键）。
//
// 用法：
//   go run ./cmd/migrate -sqlite "C:/Users/xxx/.jikeai-clone-data/jikeai.db" \
//                        -pg "host=127.0.0.1 port=5432 user=postgres password=postgres dbname=jikeai sslmode=disable"

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	sqlitePath := flag.String("sqlite", "", "SQLite 文件路径")
	pgDSN := flag.String("pg", "", "PostgreSQL DSN")
	flag.Parse()

	if *sqlitePath == "" || *pgDSN == "" {
		fmt.Println("用法: migrate -sqlite <path> -pg <dsn>")
		os.Exit(1)
	}

	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}

	src, err := gorm.Open(sqlite.Open(*sqlitePath), cfg)
	if err != nil {
		log.Fatalf("连 SQLite 失败: %v", err)
	}
	dst, err := gorm.Open(postgres.Open(*pgDSN), cfg)
	if err != nil {
		log.Fatalf("连 PostgreSQL 失败: %v", err)
	}

	// 顺序即外键依赖顺序：先主表后从表
	tables := []string{
		"users",
		"model_configs",
		"system_settings",
		"projects",
		"episodes",
		"characters",
		"scenes",
		"items",
		"storyboards",
		"first_frames",
		"generation_tasks",
		"point_transactions",
	}

	total := 0
	for _, t := range tables {
		// 用 map 读，字段名跟表走，不依赖模型定义
		var rows []map[string]interface{}
		if err := src.Table(t).Find(&rows).Error; err != nil {
			log.Printf("  %-22s 读取失败（可能不存在）: %v", t, err)
			continue
		}
		if len(rows) == 0 {
			log.Printf("  %-22s 0 行", t)
			continue
		}

		// 目标表先清空，避免重复导入时主键冲突
		if err := dst.Exec(fmt.Sprintf("DELETE FROM %s", t)).Error; err != nil {
			log.Printf("  %-22s 清空失败: %v", t, err)
		}

		// SQLite 没有布尔类型，存的是 0/1 整数；PG 的 bool 列不认整数，
		// 直接塞会报 "unable to encode 0 into binary format for bool"。
		// 先问 PG 这张表哪些列是 bool，再把 0/1 转成 false/true。
		boolCols := map[string]bool{}
		var colRows []struct {
			ColumnName string
			DataType   string
		}
		dst.Raw(`SELECT column_name, data_type FROM information_schema.columns
		         WHERE table_name = ? AND data_type = 'boolean'`, t).Scan(&colRows)
		for _, c := range colRows {
			boolCols[c.ColumnName] = true
		}

		// 主键也要过一遍：SQLite 时期的预设数据用的是 "preset-llm-openai"
		// 这种可读 ID，PG 的 uuid 列直接拒绝。非 UUID 的一律换成新 UUID，
		// 关联关系靠外键字段（episode_id 等）维系，不依赖主键的字面值。
		idIsUUID := false
		var idType struct {
			DataType string
		}
		dst.Raw(`SELECT data_type FROM information_schema.columns
		         WHERE table_name = ? AND column_name = 'id'`, t).Scan(&idType)
		idIsUUID = idType.DataType == "uuid"

		ok := 0
		failed := 0
		for _, row := range rows {
			if idIsUUID {
				if id, has := row["id"].(string); has && !looksLikeUUID(id) {
					row["id"] = newUUID()
				}
			}
			for col := range boolCols {
				if v, has := row[col]; has {
					switch n := v.(type) {
					case int64:
						row[col] = n != 0
					case int:
						row[col] = n != 0
					case float64:
						row[col] = n != 0
					}
				}
			}
			if err := dst.Table(t).Create(row).Error; err != nil {
				if failed < 3 {
					log.Printf("  %-22s 插入失败: %v", t, err)
				}
				failed++
				continue
			}
			ok++
		}
		log.Printf("  %-22s %d/%d 行", t, ok, len(rows))
		total += ok
	}

	log.Printf("迁移完成，共 %d 行", total)
}

// looksLikeUUID 粗判 8-4-4-4-12 格式
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// newUUID 生成 v4 UUID（不引第三方包，够用）
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
