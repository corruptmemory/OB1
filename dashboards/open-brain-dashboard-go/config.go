package main

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Ollama   OllamaConfig   `toml:"ollama"`
	MCP      MCPConfig      `toml:"mcp"`
}

type MCPConfig struct {
	Listen    string `toml:"listen"`
	AccessKey string `toml:"access_key"`
}

type ServerConfig struct {
	Listen string `toml:"listen"`
}

type DatabaseConfig struct {
	URL string `toml:"url"`
}

type OllamaConfig struct {
	URL            string `toml:"url"`
	EmbeddingModel string `toml:"embedding_model"`
	ChatModel      string `toml:"chat_model"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Listen: "127.0.0.1:8082",
		},
		Database: DatabaseConfig{
			URL: "postgres://openbrain:CHANGE_ME@home-server:5432/openbrain?sslmode=disable",
		},
		Ollama: OllamaConfig{
			URL:            "http://home-server:11434",
			EmbeddingModel: "mxbai-embed-large",
			ChatModel:      "qwen2.5:3b",
		},
		MCP: MCPConfig{
			Listen:    "127.0.0.1:8001",
			AccessKey: "",
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = "127.0.0.1:8080"
	}
	if cfg.Ollama.EmbeddingModel == "" {
		cfg.Ollama.EmbeddingModel = "mxbai-embed-large"
	}
	return &cfg, nil
}

func WriteConfig(path string, cfg *Config) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}
