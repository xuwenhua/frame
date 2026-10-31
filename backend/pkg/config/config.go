package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App      AppConfig      `yaml:"app"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	JWT      JWTConfig      `yaml:"jwt"`
	Points   PointsConfig   `yaml:"points"`
	AI       AIConfig       `yaml:"ai"`
}

type AppConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Port    int    `yaml:"port"`
	Mode    string `yaml:"mode"`
	// 生成产物转存目录（上游 URL 24 小时失效，SUCCESS 后立即下载到这里）
	ResultDir string `yaml:"result_dir"`
	// 产物对外访问前缀，需与上面的目录对应
	ResultURLPrefix string `yaml:"result_url_prefix"`
	// 任务并发上限：一个任务一个协程，上游并发由网关自己排队，本地不节流
	MaxConcurrentTasks int `yaml:"max_concurrent_tasks"`
	// 分镜草图专用图像模型 ID（留空则回退到图像分类的默认模型）。
	// 草图要的是「黑白线条、构图准确」，跟出资产图的模型诉求不同，
	// 所以单独指定，不动全局默认图像模型。
	SketchModelID string `yaml:"sketch_model_id"`
}

type DatabaseConfig struct {
	Driver   string `yaml:"driver"`
	DSN      string `yaml:"dsn"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
}

type RedisConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type JWTConfig struct {
	Secret string `yaml:"secret"`
	Expire int    `yaml:"expire"`
}

type PointsConfig struct {
	IdentifyCost  int `yaml:"identify_cost"`
	ImagePerCost  int `yaml:"image_per_cost"`
	VideoPerCost  int `yaml:"video_per_cost"`
}

type AIConfig struct {
	OpenAI    OpenAIConfig    `yaml:"openai"`
	RunningHub RunningHubConfig `yaml:"runninghub"`
	Doubao    DoubaoConfig    `yaml:"doubao"`
}

type OpenAIConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

type RunningHubConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
}

type DoubaoConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &cfg, nil
}
