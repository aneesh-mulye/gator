package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	DbURL           string `json:"db_url"`
	CurrentUserName string `json:"current_user_name"`
}

const configFilename = "gatorconfig.json"

func Read() (Config, error) {
	configFilePath, err := getConfigFilePath()
	if err != nil {
		return Config{}, err
	}

	rawData, err := os.ReadFile(configFilePath)
	if err != nil {
		return Config{}, fmt.Errorf("Error reading user config file: %w", err)
	}

	var config Config
	err = json.Unmarshal(rawData, &config)
	if err != nil {
		return Config{}, fmt.Errorf("Error reading JSON in config: %w", err)
	}
	return config, nil
}

func (c *Config) SetUser(username string) error {
	c.CurrentUserName = username

	err := writeConfig(*c)
	if err != nil {
		return err
	}

	return nil
}

func getConfigFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("Error getting user config directory: %w", err)
	}

	return filepath.Join(configDir, configFilename), nil
}

func writeConfig(config Config) error {
	configFilePath, err := getConfigFilePath()
	if err != nil {
		return fmt.Errorf("Error writing config file; couldn't get path: %w", err)
	}

	dataBuffer, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("Error writing config file: couldn't marshal to JSON: %w", err)
	}

	err = os.WriteFile(configFilePath, dataBuffer, 0644)
	if err != nil {
		return fmt.Errorf("Error writing config file; write failed: %w", err)
	}

	return nil
}
