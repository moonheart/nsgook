// Package config holds NSGook's persistent YAML configuration and extension filtering.
package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// BackdropStyle selects the translucent window backdrop on Windows.
type BackdropStyle string

const (
	BackdropAuto    BackdropStyle = "auto"
	BackdropNone    BackdropStyle = "none"
	BackdropMica    BackdropStyle = "mica"
	BackdropAcrylic BackdropStyle = "acrylic"
	BackdropTabbed  BackdropStyle = "tabbed"
)

// ThemeMode selects light, dark, or system-following appearance.
type ThemeMode string

const (
	ThemeSystem ThemeMode = "system"
	ThemeLight  ThemeMode = "light"
	ThemeDark   ThemeMode = "dark"
)

// FontPreviewMode selects the default preview mode in the font viewer.
type FontPreviewMode string

const (
	FontModePangram FontPreviewMode = "pangram"
	FontModeGlyphs  FontPreviewMode = "glyphs"
)

// Config holds all user-editable settings for NSGook.
type Config struct {
	ShowTrayIcon      bool          `yaml:"show_tray_icon"`
	UseTransparency   bool          `yaml:"use_transparency"`
	WindowBackdrop    BackdropStyle `yaml:"window_backdrop"`
	Topmost           bool          `yaml:"topmost"`
	ShowInTaskbar     bool          `yaml:"show_in_taskbar"`
	AutoCloseHolding  bool          `yaml:"auto_close_holding"`
	CloseOnLostFocus  bool          `yaml:"close_on_lost_focus"`
	ShowReload        bool          `yaml:"show_reload"`
	AutoReload        bool          `yaml:"auto_reload"`
	FocusWindowOnOpen bool          `yaml:"focus_window_on_open"`
	Theme             ThemeMode     `yaml:"theme"`
	Language          string        `yaml:"language"`

	UseExtensionAllowlist bool     `yaml:"use_extension_allowlist"`
	ExtensionAllowlist    []string `yaml:"extension_allowlist"`
	ExtensionBlocklist    []string `yaml:"extension_blocklist"`

	ShowLineNumbers  bool            `yaml:"show_line_numbers"`
	WordWrap         bool            `yaml:"word_wrap"`
	TextFontSize     float32         `yaml:"text_font_size"`
	TextFontFamily   string          `yaml:"text_font_family"`
	ImageCheckeredBg bool            `yaml:"image_checkered_bg"`
	RenderSvgWeb     bool            `yaml:"render_svg_web"`
	VideoLoop        bool            `yaml:"video_loop"`
	VideoVolume      float32         `yaml:"video_volume"`
	MarkdownShowToc  bool            `yaml:"markdown_show_toc"`
	FontPreviewMode  FontPreviewMode `yaml:"font_preview_mode"`
}

// Default returns NSGook's default configuration, aligned with QuickLook defaults.
func Default() Config {
	return Config{
		ShowTrayIcon:          true,
		UseTransparency:       true,
		WindowBackdrop:        BackdropAcrylic,
		Topmost:               false,
		ShowInTaskbar:         false,
		AutoCloseHolding:      true,
		CloseOnLostFocus:      false,
		ShowReload:            false,
		AutoReload:            false,
		FocusWindowOnOpen:     false,
		Theme:                 ThemeSystem,
		Language:              "",
		UseExtensionAllowlist: false,
		ExtensionAllowlist:    nil,
		ExtensionBlocklist:    []string{".insv"},
		ShowLineNumbers:       true,
		WordWrap:              true,
		TextFontSize:          14,
		TextFontFamily:        "",
		ImageCheckeredBg:      true,
		RenderSvgWeb:          true,
		VideoLoop:             false,
		VideoVolume:           1.0,
		MarkdownShowToc:       false,
		FontPreviewMode:       FontModePangram,
	}
}

// Clone returns a deep copy of c.
func (c Config) Clone() Config {
	c.ExtensionAllowlist = slices.Clone(c.ExtensionAllowlist)
	c.ExtensionBlocklist = slices.Clone(c.ExtensionBlocklist)
	return c
}

// Normalize clamps numeric values and canonicalizes enum and extension fields.
func (c *Config) Normalize() {
	switch c.WindowBackdrop {
	case BackdropAuto, BackdropNone, BackdropMica, BackdropAcrylic, BackdropTabbed:
	default:
		c.WindowBackdrop = BackdropAcrylic
	}
	switch c.Theme {
	case ThemeSystem, ThemeLight, ThemeDark:
	default:
		c.Theme = ThemeSystem
	}
	switch c.FontPreviewMode {
	case FontModePangram, FontModeGlyphs:
	default:
		c.FontPreviewMode = FontModePangram
	}
	if c.TextFontSize < 8 || c.TextFontSize > 48 {
		c.TextFontSize = 14
	}
	if c.VideoVolume < 0 {
		c.VideoVolume = 0
	} else if c.VideoVolume > 1 {
		c.VideoVolume = 1
	}
	c.ExtensionAllowlist = NormalizeExtensions(c.ExtensionAllowlist)
	c.ExtensionBlocklist = NormalizeExtensions(c.ExtensionBlocklist)
}

// IsExtensionAllowed reports whether path passes the configured allowlist/blocklist.
// Empty paths, CLSID paths (::), and files without extensions are always allowed.
func (c Config) IsExtensionAllowed(path string) bool {
	if path == "" || strings.HasPrefix(path, "::") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" || ext == "." {
		return true
	}
	if c.UseExtensionAllowlist {
		allow := NormalizeExtensions(c.ExtensionAllowlist)
		if len(allow) == 0 {
			return true
		}
		return slices.Contains(allow, ext)
	}
	block := NormalizeExtensions(c.ExtensionBlocklist)
	return !slices.Contains(block, ext)
}

// ParseExtensionList splits a comma- or semicolon-separated extension string and normalizes entries.
func ParseExtensionList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	return NormalizeExtensions(parts)
}

// FormatExtensionList joins normalized extensions for text field display.
func FormatExtensionList(exts []string) string {
	return strings.Join(NormalizeExtensions(exts), ", ")
}

// NormalizeExtensions normalizes and deduplicates extensions into lowercase ".ext" form.
func NormalizeExtensions(exts []string) []string {
	if len(exts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(exts))
	out := make([]string, 0, len(exts))
	for _, item := range exts {
		norm := normalizeExt(item)
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out
}

func normalizeExt(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, ".") {
		s = "." + s
	}
	body := s[1:]
	if body == "" {
		return ""
	}
	for _, r := range body {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return ""
		}
	}
	return s
}

// DefaultConfigPath returns the config file path, using portable UserData/ when present beside the executable.
func DefaultConfigPath(dataDir string) string {
	if exe, err := os.Executable(); err == nil {
		portableDir := filepath.Join(filepath.Dir(exe), "UserData")
		if info, statErr := os.Stat(portableDir); statErr == nil && info.IsDir() {
			return filepath.Join(portableDir, "config.yaml")
		}
	}
	if dataDir == "" {
		if dir, err := os.UserConfigDir(); err == nil && dir != "" {
			dataDir = filepath.Join(dir, "NSGook")
		} else {
			dataDir = "."
		}
	}
	return filepath.Join(dataDir, "config.yaml")
}

// Store persists Config in YAML and notifies listeners when updated.
type Store struct {
	mu       sync.RWMutex
	path     string
	cfg      Config
	onChange func(Config)
}

// NewStore opens or creates a YAML-backed Config Store at path.
// An empty path keeps configuration in memory (useful in tests).
func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		cfg:  Default(),
	}
	if path != "" {
		if err := s.load(); err != nil {
			return s, err
		}
	}
	return s, nil
}

// Path returns the backing config file path (or "" if in-memory).
func (s *Store) Path() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

// Dir returns the directory containing the config file (or ".").
func (s *Store) Dir() string {
	p := s.Path()
	if p == "" {
		return "."
	}
	return filepath.Dir(p)
}

// SetOnChange installs a listener called after every Update.
func (s *Store) SetOnChange(fn func(Config)) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// Get returns a copy of the current configuration.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

// Update mutates the config, normalizes it, writes it to disk, and notifies listeners.
func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	next := s.cfg.Clone()
	fn(&next)
	next.Normalize()
	s.cfg = next
	path := s.path
	cb := s.onChange
	s.mu.Unlock()

	var err error
	if path != "" {
		err = writeYAMLAtomic(path, next)
	}
	if cb != nil {
		cb(next.Clone())
	}
	return err
}

// IsExtensionAllowed checks path against the current configuration.
func (s *Store) IsExtensionAllowed(path string) bool {
	return s.Get().IsExtensionAllowed(path)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return writeYAMLAtomic(s.path, s.cfg)
	}
	if err != nil {
		return err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	cfg.Normalize()
	s.cfg = cfg
	return nil
}

func writeYAMLAtomic(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// On Windows, os.Rename replaces existing files atomically on Go 1.20+,
		// but fall back to direct write if a cross-device or lock error occurs.
		_ = os.Remove(tmp)
		return os.WriteFile(path, out, 0o644)
	}
	return nil
}
