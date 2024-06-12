package config

import (
	"fmt"
	"net/url"
)

// API configuration containing the base URL and query parameters
type APIConfig struct {
	BaseURL     string            `mapstructure:"base-url" json:"base-url"`
	QueryParams map[string]string `mapstructure:"query-parameters" json:"query-parameters"`
	HTTPHeaders map[string]string `mapstructure:"http-headers" json:"http-headers"`
}

func (c *APIConfig) Validate() error {
	if _, err := url.ParseRequestURI(c.BaseURL); err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	return nil
}
