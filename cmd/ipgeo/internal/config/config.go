package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/sources"
	"gopkg.in/yaml.v3"
)

const (
	defaultHTTPTimeout  = 30 * time.Minute
	defaultRetryWaitMin = 0 * time.Second
	defaultRetryWaitMax = 3 * time.Second
	defaultRetryMax     = 1
)

type HTTPConfig struct {
	Timeout      string `yaml:"timeout"`
	RetryWaitMin string `yaml:"retryWaitMin"`
	RetryWaitMax string `yaml:"retryWaitMax"`
	RetryMax     *int   `yaml:"retryMax"`
}

type SourceEntry = sources.Entry

type Config struct {
	HTTP    HTTPConfig    `yaml:"http"`
	Sources []SourceEntry `yaml:"sources"`
	homeDir string
}

func (c *Config) HTTPTimeout() time.Duration {
	timeout, err := parseHTTPTimeout(c.HTTP.Timeout)
	if err != nil {
		return defaultHTTPTimeout
	}
	return timeout
}

func (c *Config) HTTPRetryWaitMin() time.Duration {
	d, ok, err := parseRetryDuration(c.HTTP.RetryWaitMin, "retryWaitMin")
	if err != nil || !ok {
		return defaultRetryWaitMin
	}
	return d
}

func (c *Config) HTTPRetryWaitMax() time.Duration {
	d, ok, err := parseRetryDuration(c.HTTP.RetryWaitMax, "retryWaitMax")
	if err != nil || !ok {
		return defaultRetryWaitMax
	}
	return d
}

func (c *Config) HTTPRetryMax() int {
	if c.HTTP.RetryMax == nil {
		return defaultRetryMax
	}
	return *c.HTTP.RetryMax
}

func (c *Config) HomeDir() string {
	return c.homeDir
}

func (c *Config) SourcePath(filename string) string {
	return filepath.Join(c.homeDir, filename)
}

func resolveHomeDir() (string, error) {
	if h := os.Getenv("IPGEO_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ipgeo"), nil
}

func Load() (*Config, error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return nil, err
	}

	cfgPath := filepath.Join(homeDir, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if mkErr := os.MkdirAll(homeDir, 0o755); mkErr != nil {
			return nil, mkErr
		}
		if writeErr := os.WriteFile(cfgPath, []byte(defaultConfig), 0o644); writeErr != nil {
			return nil, writeErr
		}
		data = []byte(defaultConfig)
	}
	if strings.TrimSpace(string(data)) == "" {
		if writeErr := os.WriteFile(cfgPath, []byte(defaultConfig), 0o644); writeErr != nil {
			return nil, writeErr
		}
		data = []byte(defaultConfig)
	}

	return loadFromData(data, homeDir)
}

func loadFromData(data []byte, homeDir string) (*Config, error) {
	cfg := &Config{homeDir: homeDir}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if _, err := parseHTTPTimeout(c.HTTP.Timeout); err != nil {
		return err
	}
	retryWaitMin, minOk, err := parseRetryDuration(c.HTTP.RetryWaitMin, "retryWaitMin")
	if err != nil {
		return err
	}
	retryWaitMax, maxOk, err := parseRetryDuration(c.HTTP.RetryWaitMax, "retryWaitMax")
	if err != nil {
		return err
	}
	if minOk && maxOk && retryWaitMin > retryWaitMax {
		return fmt.Errorf("config http.retryWaitMin (%s) must be <= http.retryWaitMax (%s)", c.HTTP.RetryWaitMin, c.HTTP.RetryWaitMax)
	}
	if c.HTTP.RetryMax != nil && *c.HTTP.RetryMax < 0 {
		return errors.New("config http.retryMax must be >= 0")
	}
	if len(c.Sources) == 0 {
		return errors.New("config sources must contain at least one source")
	}
	sourceNames := make(map[string]int, len(c.Sources))
	filenames := make(map[string]string, len(c.Sources))
	for i := range c.Sources {
		source := &c.Sources[i]
		prefix := fmt.Sprintf("config sources[%d]", i)
		source.Name = strings.TrimSpace(source.Name)
		if source.Name == "" {
			return fmt.Errorf("%s name is required", prefix)
		}
		if prev, ok := sourceNames[source.Name]; ok {
			return fmt.Errorf("%s name %q conflicts with config sources[%d] name", prefix, source.Name, prev)
		}
		sourceNames[source.Name] = i
		source.Type = strings.TrimSpace(source.Type)
		if source.Type == "" {
			return fmt.Errorf("%s type is required", prefix)
		}
		if !sources.IsSupportedType(source.Type) {
			return fmt.Errorf("%s type %q is not supported", prefix, source.Type)
		}
		source.Filename = strings.TrimSpace(source.Filename)
		if source.Filename == "" {
			return fmt.Errorf("%s filename is required", prefix)
		}
		if err := c.validateSourceFilename(source.Filename, prefix+" filename"); err != nil {
			return err
		}
		if err := c.addSourceFilename(filenames, source.Filename, prefix+" filename"); err != nil {
			return err
		}
		if len(source.URLs) == 0 {
			return fmt.Errorf("%s urls must contain at least one URL", prefix)
		}
		source.CompanionFilename = strings.TrimSpace(source.CompanionFilename)
		hasCompanionFilename := source.CompanionFilename != ""
		hasCompanionURLs := len(source.CompanionURLs) > 0
		if hasCompanionFilename != hasCompanionURLs {
			return fmt.Errorf("%s companion_filename and companion_urls must be configured together", prefix)
		}
		if hasCompanionFilename {
			if err := c.validateSourceFilename(source.CompanionFilename, prefix+" companion_filename"); err != nil {
				return err
			}
			if err := c.addSourceFilename(filenames, source.CompanionFilename, prefix+" companion_filename"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Config) validateSourceFilename(filename, field string) error {
	home := filepath.Clean(c.homeDir)
	path := filepath.Clean(filepath.Join(home, filename))
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must resolve to a file under IPGEO_HOME", field)
	}
	return nil
}

func (c *Config) addSourceFilename(filenames map[string]string, filename, field string) error {
	key := sourceFilenameConflictKey(c.homeDir, filename, runtime.GOOS)
	if prev, ok := filenames[key]; ok {
		return fmt.Errorf("%s %q conflicts with %s", field, filename, prev)
	}
	filenames[key] = fmt.Sprintf("%s %q", field, filename)
	return nil
}

func sourceFilenameConflictKey(homeDir, filename, goos string) string {
	key := filepath.Clean(filepath.Join(homeDir, filename))
	if goos == "windows" || goos == "darwin" {
		return strings.ToLower(key)
	}
	return key
}

func parseHTTPTimeout(value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return defaultHTTPTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("config http.timeout must be a valid Go duration: %w", err)
	}
	if timeout <= 0 {
		return 0, errors.New("config http.timeout must be greater than 0")
	}
	return timeout, nil
}

func parseRetryDuration(value, fieldName string) (time.Duration, bool, error) {
	if strings.TrimSpace(value) == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, false, fmt.Errorf("config http.%s must be a valid Go duration: %w", fieldName, err)
	}
	if d < 0 {
		return 0, false, fmt.Errorf("config http.%s must be >= 0", fieldName)
	}
	return d, true, nil
}
